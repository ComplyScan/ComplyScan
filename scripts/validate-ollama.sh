#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
model=${COMPLYSCAN_OLLAMA_MODEL:-qwen3.5:9b}
output_directory=${COMPLYSCAN_VALIDATION_DIR:-${repository_root}/.complyscan/validation/ollama}
fixture_names=${COMPLYSCAN_VALIDATION_FIXTURES:-"go python typescript"}

command -v go >/dev/null 2>&1 || {
	printf '%s\n' "Error: Go is required to build the current ComplyScan source." >&2
	exit 1
}
command -v ollama >/dev/null 2>&1 || {
	printf '%s\n' "Error: Ollama is not installed. Run complyscan setup when installation is possible." >&2
	exit 1
}
ollama list | awk -v wanted="$model" 'NR > 1 && $1 == wanted { found = 1 } END { exit !found }' || {
	printf 'Error: Ollama model %s is not available. Run: ollama pull %s\n' "$model" "$model" >&2
	exit 1
}
[ -x /usr/bin/time ] || {
	printf '%s\n' "Error: /usr/bin/time is required to record resource measurements." >&2
	exit 1
}

mkdir -p "$output_directory"
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
metrics_path=${output_directory}/${timestamp}-metrics.txt
summary_path=${output_directory}/${timestamp}-summary.txt
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-ollama-validation.XXXXXX")
binary=${temporary_directory}/complyscan
saved_reports_path=${temporary_directory}/saved-reports.txt
: >"$saved_reports_path"

cleanup() {
	rm -f "$binary" "$saved_reports_path"
	rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

printf 'Building current ComplyScan source...\n'
(cd "$repository_root" && go build -trimpath -o "$binary" ./cmd/complyscan)

{
	printf 'ComplyScan CLI resource use is reported by /usr/bin/time below.\n'
	printf 'The separately running Ollama model allocation is reported by ollama ps after the scan.\n\n'
} >"$metrics_path"
: >"$summary_path"

for fixture_name in $fixture_names; do
	case "$fixture_name" in
	go | python | typescript) ;;
	*)
		printf 'Error: unsupported validation fixture %s (choose go, python, or typescript).\n' "$fixture_name" >&2
		exit 1
		;;
	esac
	fixture=${repository_root}/testdata/technical-context-${fixture_name}
	result_path=${output_directory}/${timestamp}-${fixture_name}-report.json
	printf 'Validating %s against %s...\n' "$model" "$fixture"
	printf '\n=== %s fixture ===\n' "$fixture_name" >>"$metrics_path"
	case "$(uname -s)" in
		Darwin)
			/usr/bin/time -l "$binary" review "$fixture" --provider ollama --ollama-model "$model" --refresh-review --format json --no-report >"$result_path" 2>>"$metrics_path"
			;;
		Linux)
			/usr/bin/time -v "$binary" review "$fixture" --provider ollama --ollama-model "$model" --refresh-review --format json --no-report >"$result_path" 2>>"$metrics_path"
			;;
		*)
			printf 'Error: live resource validation is not implemented for %s.\n' "$(uname -s)" >&2
			exit 1
			;;
	esac
	printf '\n%s fixture:\n' "$fixture_name" >>"$summary_path"
	(cd "$repository_root" && go run ./scripts/validate_ollama_result.go "$result_path" "$model") >>"$summary_path"
	printf 'Saved %s report: %s\n' "$fixture_name" "$result_path" >>"$saved_reports_path"
done

{
	printf '\nOllama loaded-model allocation after scan:\n'
	ollama ps
} >>"$metrics_path"

cat "$summary_path"
printf '\n'
cat "$saved_reports_path"
printf 'Saved metrics: %s\nSaved summary: %s\n' "$metrics_path" "$summary_path"
