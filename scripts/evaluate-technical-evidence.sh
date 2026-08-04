#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-technical-evaluation.XXXXXX")
binary=${temporary_directory}/evaluate-technical-evidence

cleanup() {
	rm -f "$binary"
	rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

cd "$repository_root"
go build -trimpath -o "$binary" ./scripts/evaluate-technical-evidence
"$binary" "$@"
