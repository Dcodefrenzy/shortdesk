#!/bin/sh
# Installs the latest Shortdesk on macOS or Linux:
#   curl -fsSL https://raw.githubusercontent.com/Dcodefrenzy/shortdesk/main/scripts/install.sh | sh
# Set SHORTDESK_INSTALL_DIR to choose where the binary goes.
set -eu

repo="Dcodefrenzy/shortdesk"
base="${SHORTDESK_DOWNLOAD_URL:-https://github.com/$repo/releases/latest/download}"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Shortdesk: this script is for macOS and Linux. On Windows, run in PowerShell:" >&2
     echo "  irm https://raw.githubusercontent.com/$repo/main/scripts/install.ps1 | iex" >&2
     exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Shortdesk: unsupported processor $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${SHORTDESK_INSTALL_DIR:-}" ]; then
  dir="$SHORTDESK_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "Downloading Shortdesk for ${os}/${arch}..."
url="$base/shortdesk_${os}_${arch}.tar.gz"
if ! curl -fsSL -o "$tmp/shortdesk.tar.gz" "$url"; then
  echo "Shortdesk: couldn't download $url" >&2
  exit 1
fi
tar -xzf "$tmp/shortdesk.tar.gz" -C "$tmp"
install -m 0755 "$tmp/shortdesk" "$dir/shortdesk"
echo "Installed $("$dir/shortdesk" --version) to $dir/shortdesk"

case ":$PATH:" in
  *":$dir:"*) cmd=shortdesk ;;
  *) cmd="$dir/shortdesk"
     shell_rc="$HOME/.profile"
     case "${SHELL:-}" in */zsh) shell_rc="$HOME/.zshrc" ;; */bash) shell_rc="$HOME/.bashrc" ;; esac
     echo
     echo "$dir isn't on your PATH. To run plain 'shortdesk', add it with:"
     echo "  echo 'export PATH=\"$dir:\$PATH\"' >> $shell_rc && . $shell_rc" ;;
esac

echo
if ! command -v docker >/dev/null 2>&1; then
  echo "Shortdesk needs Docker. Install it first: https://docs.docker.com/get-docker/"
  echo "(Docker Desktop on Mac.) Then start Docker and run:  $cmd up"
else
  echo "Next: make sure Docker is running, then run:  $cmd up"
fi
