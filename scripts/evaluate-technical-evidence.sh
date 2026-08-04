#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "${script_directory}/.." && pwd)

cd "$repository_root"
exec go run ./scripts/evaluate-technical-evidence "$@"
