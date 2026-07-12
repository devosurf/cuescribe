#!/bin/sh
set -eu

repo_dir="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

fake_bin="$tmp/fake-bin"
home="$tmp/home"
install_dir="$home/.local/bin"
payload="$tmp/cuescribe"
mkdir -p "$fake_bin" "$home"

cat > "$payload" <<'EOF'
#!/bin/sh
exit 0
EOF

cat > "$fake_bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *) exit 2 ;;
esac
EOF

cat > "$fake_bin/shasum" <<'EOF'
#!/bin/sh
printf '%s\n' "$FAKE_SHA256"
EOF

cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) shift; out="$1" ;;
    http*) url="$1" ;;
  esac
  shift
done
case "$url" in
  *manifest.json)
    printf '{"binary_url":"https://github.com/devosurf/cuescribe/releases/download/test/cuescribe","binary_sha256":"%s"}\n' "$FAKE_SHA256" > "$out"
    ;;
  https://github.com/*)
    cp "$FAKE_BINARY" "$out"
    ;;
  *)
    echo "unexpected URL: $url" >&2
    exit 2
    ;;
esac
EOF

chmod +x "$payload" "$fake_bin/uname" "$fake_bin/shasum" "$fake_bin/curl"

output="$({
  HOME="$home" \
  PATH="$fake_bin:$PATH" \
  FAKE_BINARY="$payload" \
  FAKE_SHA256="test-checksum" \
    sh "$repo_dir/install.sh" --no-setup --install-dir "$install_dir"
} 2>&1)"

assert_contains() {
  case "$output" in
    *"$1"*) ;;
    *)
      printf 'installer output missing %s:\n%s\n' "$1" "$output" >&2
      exit 1
      ;;
  esac
}

assert_contains 'Cuescribe was installed in ~/.local/bin, which is not in your current PATH.'
assert_contains 'export PATH="$HOME/.local/bin:$PATH"'
assert_contains 'echo '\''export PATH="$HOME/.local/bin:$PATH"'\'' >> "$HOME/.zshrc"'
assert_contains "$install_dir/cuescribe \"https://www.youtube.com/watch?v=jK-iJbM7Ow0\""
assert_contains 'Keep URLs in quotes. In zsh, ? is a wildcard when unquoted.'

if [ ! -x "$install_dir/cuescribe" ]; then
  printf 'installed binary is missing or not executable: %s\n' "$install_dir/cuescribe" >&2
  exit 1
fi

printf 'installer guidance test: PASS\n'
