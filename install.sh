#!/bin/sh
set -eu

repo="devosurf/cuescribe"
version="latest"
install_dir=""
run_setup=1
yes=0

while [ "$#" -gt 0 ]; do
	case "$1" in
		--no-setup)
			run_setup=0
			;;
		--yes)
			yes=1
			;;
		--install-dir)
			shift
			install_dir="${1:-}"
			;;
		--version)
			shift
			version="${1:-}"
			;;
		-h|--help)
			echo "usage: install.sh [--no-setup] [--yes] [--install-dir DIR] [--version VERSION]"
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			exit 2
			;;
	esac
	shift
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
if [ "$os" != "darwin" ] || [ "$arch" != "arm64" ]; then
	echo "Error: unsupported platform $os/$arch." >&2
	echo "Fix: Cuescribe v1 supports macOS Apple Silicon only." >&2
	exit 1
fi

if [ -z "$install_dir" ]; then
	if [ -w /usr/local/bin ]; then
		install_dir="/usr/local/bin"
	else
		install_dir="$HOME/.local/bin"
	fi
fi

if [ "$version" = "latest" ]; then
	manifest_url="https://github.com/$repo/releases/latest/download/manifest.json"
else
	manifest_url="https://github.com/$repo/releases/download/$version/manifest.json"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
manifest="$tmp/manifest.json"
binary="$tmp/cuescribe"

echo "downloading manifest: $manifest_url"
curl -fsSL "$manifest_url" -o "$manifest"

binary_url="$(sed -n 's/.*"binary_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)"
binary_sha256="$(sed -n 's/.*"binary_sha256"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)"

if [ -z "$binary_url" ] || [ -z "$binary_sha256" ]; then
	echo "Error: manifest is missing binary_url or binary_sha256." >&2
	exit 1
fi

echo "downloading binary: $binary_url"
curl -fL --progress-bar "$binary_url" -o "$binary"

actual_sha256="$(shasum -a 256 "$binary" | awk '{print $1}')"
if [ "$actual_sha256" != "$binary_sha256" ]; then
	echo "Error: checksum mismatch." >&2
	echo "Expected: $binary_sha256" >&2
	echo "Actual:   $actual_sha256" >&2
	exit 1
fi

mkdir -p "$install_dir"
install -m 0755 "$binary" "$install_dir/cuescribe"
echo "installed $install_dir/cuescribe"

if [ "$run_setup" -eq 1 ]; then
	if [ "$yes" -eq 1 ]; then
		"$install_dir/cuescribe" setup --yes
	else
		"$install_dir/cuescribe" setup
	fi
fi
