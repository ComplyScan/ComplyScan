#!/bin/sh

set -eu

if [ "$#" -ne 3 ]; then
	printf '%s\n' "Usage: ./scripts/validate-cloud-model.sh PROVIDER MODEL API_KEY_ENV" >&2
	printf '%s\n' "Providers: openai, anthropic, gemini" >&2
	exit 2
fi

provider=$1
model=$2
api_key_env=$3

case "$provider" in
openai | anthropic | gemini) ;;
*)
	printf 'Error: unsupported provider %s (choose openai, anthropic, or gemini).\n' "$provider" >&2
	exit 2
	;;
esac

case "${provider}:${model}" in
openai:gpt-5.6-sol | openai:gpt-5.6-terra | \
	anthropic:claude-opus-5 | anthropic:claude-sonnet-5 | \
	gemini:gemini-3.7-flash | gemini:gemini-3.6-flash) ;;
*)
	printf 'Error: %s/%s is not in the standard ComplyScan cloud shortlist.\n' "$provider" "$model" >&2
	exit 2
	;;
esac

case "$api_key_env" in
'' | *[!A-Za-z0-9_]*)
	printf 'Error: invalid API-key environment-variable name %s.\n' "$api_key_env" >&2
	exit 2
	;;
esac

if ! printenv "$api_key_env" >/dev/null 2>&1; then
	printf 'Error: %s is not set. Export the provider key before running this paid live evaluation.\n' "$api_key_env" >&2
	exit 2
fi

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
output_directory=${COMPLYSCAN_VALIDATION_DIR:-${repository_root}/.complyscan/validation/cloud-model}
profile_manifest=${COMPLYSCAN_PROFILE_DRAFT_MANIFEST:-${repository_root}/testdata/profile-draft-evaluation/manifest.json}
fixture_names=${COMPLYSCAN_VALIDATION_FIXTURES:-"go python typescript"}

command -v go >/dev/null 2>&1 || {
	printf '%s\n' "Error: Go is required to build the validation tools." >&2
	exit 1
}

mkdir -p "$output_directory"
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
run_directory=${output_directory}/${timestamp}-${provider}-${model}
mkdir -p "$run_directory"
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-cloud-validation.XXXXXX")
complyscan_binary=${temporary_directory}/complyscan
profile_binary=${temporary_directory}/evaluate-profile-draft
fixture_summary=${temporary_directory}/fixture-summary.txt

cleanup() {
	rm -f "$complyscan_binary" "$profile_binary" "$fixture_summary"
	rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

printf 'Building current ComplyScan validation tools...\n'
(cd "$repository_root" && go build -trimpath -o "$complyscan_binary" ./cmd/complyscan)
(cd "$repository_root" && go build -trimpath -o "$profile_binary" ./scripts/evaluate-profile-draft)

profile_result=${run_directory}/profile-draft-result.json
profile_summary=${run_directory}/profile-draft-summary.txt
printf 'Running setup-draft gate for %s/%s...\n' "$provider" "$model"
if "$profile_binary" \
		--manifest "$profile_manifest" \
		--provider "$provider" \
		--model "$model" \
		--api-key-env "$api_key_env" \
		--output "$profile_result" >"$profile_summary"; then
	cat "$profile_summary"
else
	profile_gate_exit=$?
	cat "$profile_summary"
	exit "$profile_gate_exit"
fi

technical_summary=${run_directory}/technical-review-summary.txt
: >"$technical_summary"
for fixture_name in $fixture_names; do
	case "$fixture_name" in
	go | python | typescript) ;;
	*)
		printf 'Error: unsupported validation fixture %s (choose go, python, or typescript).\n' "$fixture_name" >&2
		exit 2
		;;
	esac
	fixture=${repository_root}/testdata/technical-context-${fixture_name}
	result_path=${run_directory}/${fixture_name}-report.json
	printf 'Running technical-review gate for %s/%s against %s...\n' "$provider" "$model" "$fixture_name"
	"$complyscan_binary" review "$fixture" \
		--provider "$provider" \
		--model "$model" \
		--api-key-env "$api_key_env" \
		--refresh-review \
		--format json \
		--no-report >"$result_path"
	if {
		printf '\n=== %s fixture ===\n' "$fixture_name"
		(cd "$repository_root" && go run ./scripts/validate_ollama_result.go "$result_path" "$model")
	} >"$fixture_summary"; then
		tee -a "$technical_summary" <"$fixture_summary"
	else
		technical_gate_exit=$?
		tee -a "$technical_summary" <"$fixture_summary"
		exit "$technical_gate_exit"
	fi
done

cat >"${run_directory}/metadata.txt" <<EOF
Provider: $provider
Model: $model
Generated at: $timestamp
Credential source: environment variable $api_key_env (value not recorded)
Prompt versions and acceptance thresholds are recorded in the generated JSON reports.
EOF

printf '\nPASS: both maintained live gates completed for %s/%s.\n' "$provider" "$model"
printf 'Saved validation artifacts: %s\n' "$run_directory"
