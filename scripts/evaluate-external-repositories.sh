#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/complyscan-external-runner.XXXXXX")
binary=${temporary_directory}/evaluate-external-repositories

cleanup() {
    rm -f "$binary"
    rmdir "$temporary_directory" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

cd "$repository_root"
go build -trimpath -o "$binary" ./scripts/evaluate-external-repositories
"$binary" "$@"
