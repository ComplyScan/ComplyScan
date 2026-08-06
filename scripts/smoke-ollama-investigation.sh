#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
model=${COMPLYSCAN_OLLAMA_MODEL:-qwen3:8b}
output_directory=${COMPLYSCAN_SMOKE_DIR:-${repository_root}/.complyscan/validation/ollama-smoke}
fixture=${repository_root}/testdata/ollama-investigation-smoke
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-ollama-smoke.XXXXXX")
binary=${temporary_directory}/complyscan
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
result_path=${output_directory}/${timestamp}-report.json

cleanup() {
	rm -f "$binary"
	rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

command -v go >/dev/null 2>&1 || { printf '%s\n' "Error: Go is required." >&2; exit 1; }
command -v ollama >/dev/null 2>&1 || { printf '%s\n' "Error: Ollama is required." >&2; exit 1; }
ollama list | awk -v wanted="$model" 'NR > 1 && $1 == wanted { found = 1 } END { exit !found }' || {
	printf 'Error: Ollama model %s is unavailable. Run: ollama pull %s\n' "$model" "$model" >&2
	exit 1
}

mkdir -p "$output_directory"
printf 'Building current ComplyScan source...\n'
(cd "$repository_root" && go build -trimpath -o "$binary" ./cmd/complyscan)
printf 'Running two-target Ollama evidence smoke test with %s...\n' "$model"
"$binary" scan "$fixture" --review ollama --ollama-model "$model" --refresh-review --format json --no-report >"$result_path"
(cd "$repository_root" && go run ./scripts/validate-ollama-smoke "$result_path")
printf 'Saved smoke report: %s\n' "$result_path"
