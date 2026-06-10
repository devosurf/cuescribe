#!/bin/sh
set -eu

repo="devosurf/cuescribe"
version="latest"
install_dir=""
run_setup=1
yes=0
require_cookies=0
cookies_browser=""
cookies_profile=""

while [ "$#" -gt 0 ]; do
	case "$1" in
		--no-setup)
			run_setup=0
			;;
		--yes)
			yes=1
			;;
		--require-cookies)
			require_cookies=1
			;;
		--cookies-browser)
			shift
			cookies_browser="${1:-}"
			;;
		--cookies-profile)
			shift
			cookies_profile="${1:-}"
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
			echo "usage: install.sh [--no-setup] [--yes] [--require-cookies] [--cookies-browser BROWSER] [--cookies-profile PROFILE] [--install-dir DIR] [--version VERSION]"
			exit 0
			;;
		*)
			echo "unknown option: $1" >&2
			exit 2
			;;
	esac
	shift
done

if [ "$version" != "latest" ]; then
	case "$version" in
		""|*[!A-Za-z0-9._+-]*|*..*)
			echo "Error: invalid version: $version" >&2
			echo "Fix: pass a release tag such as v0.1.15, or omit --version for the latest release." >&2
			exit 2
			;;
	esac
fi

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

# The checksum comes from the same manifest as the URL, so it cannot protect
# against a tampered manifest; pinning the download host to GitHub can.
case "$binary_url" in
	https://github.com/*|https://objects.githubusercontent.com/*|https://release-assets.githubusercontent.com/*)
		;;
	*)
		echo "Error: binary_url in manifest is not a GitHub URL: $binary_url" >&2
		exit 1
		;;
esac

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
	set -- setup
	if [ "$yes" -eq 1 ]; then
		set -- "$@" --yes
	fi
	if [ "$require_cookies" -eq 1 ]; then
		set -- "$@" --require-cookies
	fi
	if [ -n "$cookies_browser" ]; then
		set -- "$@" --cookies-browser "$cookies_browser"
	fi
	if [ -n "$cookies_profile" ]; then
		set -- "$@" --cookies-profile "$cookies_profile"
	fi
	"$install_dir/cuescribe" "$@"
fi
