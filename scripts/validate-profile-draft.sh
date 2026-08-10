#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
model=${COMPLYSCAN_OLLAMA_MODEL:-qwen3.5:9b}
endpoint=${COMPLYSCAN_OLLAMA_ENDPOINT:-http://127.0.0.1:11434}
manifest=${COMPLYSCAN_PROFILE_DRAFT_MANIFEST:-${repository_root}/testdata/profile-draft-evaluation/manifest.json}
selected_cases=${COMPLYSCAN_PROFILE_DRAFT_CASES:-}
output_directory=${COMPLYSCAN_VALIDATION_DIR:-${repository_root}/.complyscan/validation/profile-draft}

command -v go >/dev/null 2>&1 || {
	printf '%s\n' "Error: Go is required to build the profile-draft evaluator." >&2
	exit 1
}
command -v ollama >/dev/null 2>&1 || {
	printf '%s\n' "Error: Ollama is not installed." >&2
	exit 1
}
OLLAMA_HOST="$endpoint" ollama list | awk -v wanted="$model" 'NR > 1 && $1 == wanted { found = 1 } END { exit !found }' || {
	printf 'Error: Ollama model %s is not available. Run: ollama pull %s\n' "$model" "$model" >&2
	exit 1
}
[ -x /usr/bin/time ] || {
	printf '%s\n' "Error: /usr/bin/time is required to record resource measurements." >&2
	exit 1
}
[ -f "$manifest" ] || {
	printf 'Error: profile-draft manifest does not exist: %s\n' "$manifest" >&2
	exit 1
}

mkdir -p "$output_directory"
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
result_path=${output_directory}/${timestamp}-result.json
metrics_path=${output_directory}/${timestamp}-metrics.txt
summary_path=${output_directory}/${timestamp}-summary.txt
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-profile-draft.XXXXXX")
binary=${temporary_directory}/evaluate-profile-draft

cleanup() {
	rm -f "$binary"
	rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

printf 'Building the current profile-draft evaluator...\n'
(cd "$repository_root" && go build -trimpath -o "$binary" ./scripts/evaluate-profile-draft)

set --
for case_id in $selected_cases; do
	set -- "$@" --case "$case_id"
done

{
	printf 'Profile-draft quality-gate resource measurements.\n'
	printf 'Model: %s\nEndpoint: %s\nManifest: %s\n' "$model" "$endpoint" "$manifest"
	if [ -n "$selected_cases" ]; then
		printf 'Selected cases: %s\n' "$selected_cases"
	else
		printf 'Selected cases: all\n'
	fi
	printf '\n'
} >"$metrics_path"

printf 'Validating profile drafts with %s...\n' "$model"
case "$(uname -s)" in
Darwin)
	if /usr/bin/time -l "$binary" --manifest "$manifest" --model "$model" --endpoint "$endpoint" --output "$result_path" "$@" >"$summary_path" 2>>"$metrics_path"; then
		status=0
	else
		status=$?
	fi
	;;
Linux)
	if /usr/bin/time -v "$binary" --manifest "$manifest" --model "$model" --endpoint "$endpoint" --output "$result_path" "$@" >"$summary_path" 2>>"$metrics_path"; then
		status=0
	else
		status=$?
	fi
	;;
*)
	printf 'Error: live resource validation is not implemented for %s.\n' "$(uname -s)" >&2
	exit 1
	;;
esac

{
	printf '\nOllama loaded-model allocation after evaluation:\n'
	OLLAMA_HOST="$endpoint" ollama ps
} >>"$metrics_path"

cat "$summary_path"
printf '\n'
if [ -f "$result_path" ]; then
	printf 'Saved result: %s\n' "$result_path"
fi
printf 'Saved metrics: %s\nSaved summary: %s\n' "$metrics_path" "$summary_path"
exit "$status"
