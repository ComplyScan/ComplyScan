#!/bin/sh

set -eu

repository="${COMPLYSCAN_REPOSITORY:-ComplyScan/ComplyScan}"
release_base="${COMPLYSCAN_RELEASE_BASE_URL:-https://github.com/${repository}/releases/download}"
latest_url="${COMPLYSCAN_LATEST_URL:-https://github.com/${repository}/releases/latest}"
install_dir="${COMPLYSCAN_INSTALL_DIR:-${HOME}/.local/bin}"
requested_version="${COMPLYSCAN_VERSION:-latest}"
run_setup=1

usage() {
	printf '%s\n' "Install ComplyScan from a verified GitHub release archive."
	printf '\nUsage: install.sh [--version VERSION] [--install-dir DIRECTORY] [--no-setup]\n'
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			[ "$#" -ge 2 ] || { printf '%s\n' "Error: --version requires a value" >&2; exit 2; }
			requested_version="$2"
			shift 2
			;;
		--install-dir)
			[ "$#" -ge 2 ] || { printf '%s\n' "Error: --install-dir requires a value" >&2; exit 2; }
			install_dir="$2"
			shift 2
			;;
		--no-setup)
			run_setup=0
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			printf 'Error: unknown option %s\n' "$1" >&2
			usage >&2
			exit 2
			;;
	esac
done

[ -n "$install_dir" ] || { printf '%s\n' "Error: install directory must not be empty" >&2; exit 2; }

for dependency in curl tar awk mktemp; do
	command -v "$dependency" >/dev/null 2>&1 || {
		printf 'Error: required command %s was not found\n' "$dependency" >&2
		exit 1
	}
done

case "$(uname -s)" in
	Darwin) operating_system="darwin" ;;
	Linux) operating_system="linux" ;;
	*)
		printf 'Error: unsupported operating system %s. Use a release archive instead.\n' "$(uname -s)" >&2
		exit 1
		;;
esac

case "$(uname -m)" in
	x86_64|amd64) architecture="amd64" ;;
	arm64|aarch64) architecture="arm64" ;;
	*)
		printf 'Error: unsupported architecture %s. Use a release archive instead.\n' "$(uname -m)" >&2
		exit 1
		;;
esac

if [ "$requested_version" = "latest" ]; then
	resolved_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$latest_url")"
	version_tag="${resolved_url##*/}"
	case "$version_tag" in
		v[0-9]*) ;;
		*)
			printf 'Error: could not resolve the latest ComplyScan release from %s\n' "$resolved_url" >&2
			exit 1
			;;
	esac
else
	case "$requested_version" in
		v*) version_tag="$requested_version" ;;
		*) version_tag="v${requested_version}" ;;
	esac
fi

version="${version_tag#v}"
case "$version" in
	""|*[!0-9A-Za-z._-]*)
		printf 'Error: invalid release version %s\n' "$version_tag" >&2
		exit 1
		;;
esac
archive="complyscan_${version}_${operating_system}_${architecture}.tar.gz"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/complyscan-install.XXXXXX")"
temporary_binary=""

cleanup() {
	rm -rf "$temporary_directory"
	if [ -n "$temporary_binary" ]; then
		rm -f "$temporary_binary"
	fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

printf 'Installing ComplyScan %s for %s/%s...\n' "$version_tag" "$operating_system" "$architecture"
curl -fsSL "${release_base}/${version_tag}/${archive}" -o "${temporary_directory}/${archive}"
curl -fsSL "${release_base}/${version_tag}/checksums.txt" -o "${temporary_directory}/checksums.txt"

expected_checksum="$(awk -v archive="$archive" '$2 == archive || $2 == "*" archive { print $1; exit }' "${temporary_directory}/checksums.txt")"
[ -n "$expected_checksum" ] || {
	printf 'Error: checksums.txt does not contain %s\n' "$archive" >&2
	exit 1
}

if command -v sha256sum >/dev/null 2>&1; then
	actual_checksum="$(sha256sum "${temporary_directory}/${archive}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual_checksum="$(shasum -a 256 "${temporary_directory}/${archive}" | awk '{ print $1 }')"
else
	printf '%s\n' "Error: sha256sum or shasum is required to verify the download" >&2
	exit 1
fi

if [ "$actual_checksum" != "$expected_checksum" ]; then
	printf 'Error: checksum verification failed for %s\n' "$archive" >&2
	exit 1
fi
printf '%s\n' "Verified SHA-256 checksum."

tar -xzf "${temporary_directory}/${archive}" -C "$temporary_directory"
[ -f "${temporary_directory}/complyscan" ] || {
	printf '%s\n' "Error: release archive does not contain the complyscan binary" >&2
	exit 1
}

mkdir -p "$install_dir"
temporary_binary="$(mktemp "${install_dir}/.complyscan-install.XXXXXX")"
cp "${temporary_directory}/complyscan" "$temporary_binary"
chmod 0755 "$temporary_binary"
mv "$temporary_binary" "${install_dir}/complyscan"
printf 'Installed %s\n' "${install_dir}/complyscan"

case ":${PATH}:" in
	*":${install_dir}:"*) ;;
	*)
		printf '\nAdd this directory to PATH to run ComplyScan from any terminal:\n  export PATH="%s:$PATH"\n' "$install_dir"
		;;
esac

if [ "$run_setup" -eq 1 ]; then
	if [ -r /dev/tty ]; then
		printf '%s\n' "Starting guided setup..."
		"${install_dir}/complyscan" setup --interactive </dev/tty
	else
		printf '\nNo interactive terminal was detected. Start setup later with:\n  %s setup\n' "${install_dir}/complyscan"
	fi
fi
