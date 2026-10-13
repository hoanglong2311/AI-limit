#!/usr/bin/env bash
# Build AI-limit.app (universal arm64+amd64) and a .dmg, on Linux or macOS.
# Needs: Go 1.27+, rcodesign (ad-hoc signing on Linux), genisoimage (Linux)
# or hdiutil (macOS). Output: dist/AI-limit-<version>-universal.dmg
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' mygo.json)
APP=build/darwin-universal/AI-limit.app
rm -rf build dist && mkdir -p dist
go run github.com/egoist/mygo/cmd/mygo@v0.4.8 build -platform darwin/universal -skip-dmg -skip-notarize
# Ad-hoc sign the whole bundle (seals Info.plist), no developer identity.
if command -v codesign >/dev/null; then
  codesign --force --deep --sign - "$APP"
else
  rcodesign sign "$APP"
fi
STAGE=$(mktemp -d)
cp -a "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
DMG="dist/AI-limit-$VERSION-universal.dmg"
if command -v hdiutil >/dev/null; then
  hdiutil create -volname "AI-limit $VERSION" -srcfolder "$STAGE" -ov -format UDZO "$DMG"
else
  # ISO9660 + Rock Ridge (keeps the exec bit and the Applications symlink)
  # + HFS hybrid; macOS mounts it read-only like a dmg.
  genisoimage -quiet -V "AI-limit $VERSION" -D -R -apple -no-pad -o "$DMG" "$STAGE"
fi
rm -rf "$STAGE"
(cd dist && sha256sum ./*.dmg 2>/dev/null || shasum -a 256 ./*.dmg)
