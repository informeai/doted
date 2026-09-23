#!/usr/bin/env bash
# Builds a .deb package with the doted binary and a desktop entry.
#
# Usage: packaging/linux/build-deb.sh <version> [out-dir] [arch]
#   arch is a Debian architecture: amd64 (default) or arm64.
set -euo pipefail

version=${1:?usage: build-deb.sh <version> [out-dir] [arch]}
out=$(mkdir -p "${2:-dist}" && cd "${2:-dist}" && pwd)
arch=${3:-amd64}
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

pkg="$work/doted_${version}_${arch}"
mkdir -p "$pkg/DEBIAN" "$pkg/usr/bin" "$pkg/usr/share/applications" "$pkg/usr/share/doc/doted"

# Ebiten loads X11 and OpenGL at runtime (dlopen), so the binary builds
# without cgo and doesn't depend on the build machine's glibc.
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
	go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$pkg/usr/bin/doted" .)
install -m 0644 "$root/packaging/linux/doted.desktop" "$pkg/usr/share/applications/doted.desktop"
for size in 16 24 32 48 64 128 256 512; do
	install -D -m 0644 "$root/assets/icon/png/doted-$size.png" "$pkg/usr/share/icons/hicolor/${size}x${size}/apps/doted.png"
done
install -D -m 0644 "$root/assets/icon/doted.svg" "$pkg/usr/share/icons/hicolor/scalable/apps/doted.svg"
install -m 0644 "$root/README.md" "$pkg/usr/share/doc/doted/README.md"

# Depends lists the libraries Ebiten loads at runtime, since dpkg can't see
# dlopen'ed libraries.
cat >"$pkg/DEBIAN/control" <<EOF
Package: doted
Version: $version
Section: utils
Priority: optional
Architecture: $arch
Maintainer: informeai <contato.informeai@gmail.com>
Homepage: https://github.com/informeai/doted
Installed-Size: $(du -sk "$pkg/usr" | cut -f1)
Depends: libgl1, libx11-6, libxcursor1, libxext6, libxi6, libxinerama1, libxrandr2
Description: Terminal emulator rendered with Ebitengine
 doted runs commands in a pseudo-terminal with the input line at the bottom
 and the output above it. Long-running commands can be sent to the
 background and brought back later with all of their output.
EOF

deb="$out/doted_${version}_${arch}.deb"
dpkg-deb --build --root-owner-group "$pkg" "$deb" >/dev/null
echo "$deb"
