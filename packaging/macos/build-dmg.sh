#!/usr/bin/env bash
# Builds a universal (Apple Silicon + Intel) doted.app and packs it into a
# .dmg with a shortcut to /Applications.
#
# Usage: packaging/macos/build-dmg.sh <version> [out-dir]
set -euo pipefail

version=${1:?usage: build-dmg.sh <version> [out-dir]}
out=$(mkdir -p "${2:-dist}" && cd "${2:-dist}" && pwd)
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

app="$work/dmg/doted.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

# Ebiten and creack/pty don't need cgo on macOS, so both architectures
# cross-compile from any Mac.
for arch in arm64 amd64; do
	(cd "$root" && CGO_ENABLED=0 GOOS=darwin GOARCH=$arch \
		go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/doted-$arch" .)
done
lipo -create -output "$app/Contents/MacOS/doted" "$work/doted-arm64" "$work/doted-amd64"
sed "s/@VERSION@/$version/g" "$root/packaging/macos/Info.plist" >"$app/Contents/Info.plist"
cp "$root/assets/icon/doted.icns" "$app/Contents/Resources/doted.icns"

# lipo drops the linker's signature, and Apple Silicon refuses to run
# unsigned code, so sign ad hoc (no Apple Developer ID involved).
codesign --force --sign - "$app"

ln -s /Applications "$work/dmg/Applications"
dmg="$out/doted-$version-macos-universal.dmg"
# hdiutil occasionally fails with "Resource busy" on CI machines; retry.
for attempt in 1 2 3; do
	if hdiutil create -quiet -volname "doted $version" -srcfolder "$work/dmg" -ov -format UDZO "$dmg"; then
		echo "$dmg"
		exit 0
	fi
	echo "hdiutil failed (attempt $attempt), retrying..." >&2
	sleep 5
done
exit 1
