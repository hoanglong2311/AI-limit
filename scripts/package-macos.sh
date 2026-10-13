#!/usr/bin/env bash
# Build AI-limit.app (universal arm64+amd64) and a drag-to-Applications
# UDZO .dmg, on Linux or macOS. Output: dist/AI-limit-<version>-universal.dmg
#
# Linux needs: Go 1.27+, rcodesign, mkfs.hfsplus (hfsprogs), `hfsplus` and
# `dmg` from libdmg-hfsplus, python3 with ds_store + mac_alias
# (PYTHON=/path/to/venv/python to pick one). See docs/dmg-plan.md.
# macOS needs only Go and the Xcode command line tools (hdiutil, codesign).
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' mygo.json)
VOL="AI-limit"
NAME=AI-limit.app
APP=build/darwin-universal/$NAME
DMG="dist/AI-limit-$VERSION-universal.dmg"
PY=${PYTHON:-python3}
export PATH="$PATH:/usr/sbin:/sbin"
rm -rf build dist && mkdir -p dist
# mygo version comes from go.mod.
go run github.com/egoist/mygo/cmd/mygo build -platform darwin/universal -skip-dmg -skip-notarize
# Ad-hoc sign the whole bundle (seals Info.plist), no developer identity.
if command -v codesign >/dev/null; then
  codesign --force --deep --sign - "$APP"
else
  rcodesign sign "$APP"
fi
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir "$WORK/stage"
cp -a "$APP" "$WORK/stage/"

if command -v hdiutil >/dev/null; then
  # macOS: same layout; CNIDs are inode numbers on HFS+.
  mkdir "$WORK/stage/.background"
  cp scripts/dmg/background.png "$WORK/stage/.background/"
  ln -s /Applications "$WORK/stage/Applications"
  hdiutil create -volname "$VOL" -srcfolder "$WORK/stage" -fs HFS+ -ov -format UDRW "$WORK/rw.dmg"
  MNT="$WORK/mnt"
  mkdir "$MNT"
  DEV=$(hdiutil attach -readwrite -noverify -noautoopen -nobrowse -mountpoint "$MNT" "$WORK/rw.dmg" |
    awk '/Apple_HFS/{print $1}')
  "$PY" scripts/dmg/dsstore.py "$MNT/.DS_Store" "$VOL" "$(stat -f %B "$MNT")" \
    "$(stat -f %i "$MNT/.background")" "$(stat -f %i "$MNT/.background/background.png")" "$NAME"
  # Finder opens the window on mount (finderInfo[2] = root folder).
  bless --folder "$MNT" --openfolder "$MNT" || echo "cảnh báo: bless --openfolder thất bại" >&2
  sync
  hdiutil detach "$DEV" || { sleep 3; hdiutil detach -force "$DEV"; }
  hdiutil convert "$WORK/rw.dmg" -format UDZO -o "$DMG"
else
  SIZE_MB=$(( $(du -sm "$WORK/stage" | cut -f1) + 16 ))
  RAW="$WORK/raw.img"
  dd if=/dev/zero of="$RAW" bs=1M count="$SIZE_MB" status=none
  mkfs.hfsplus -v "$VOL" "$RAW" >/dev/null
  # Fresh volume: CNIDs are handed out from 16 in creation order.
  hfsplus "$RAW" mkdir /.background                                   # CNID 16
  hfsplus "$RAW" add scripts/dmg/background.png /.background/background.png  # CNID 17
  hfsplus "$RAW" addall "$WORK/stage" / >/dev/null
  # addall copies data only; restore the exec bit of every executable.
  (cd "$WORK/stage" && find "$NAME" -type f -perm -u+x) | while read -r f; do
    hfsplus "$RAW" chmod 755 "/$f"
  done
  hfsplus "$RAW" symlink /Applications /Applications
  # Volume create date (HFS+ header +16, seconds since 1904) for the alias.
  CREATED=$(( $(od -An -t u4 --endian=big -j $((1024 + 16)) -N 4 "$RAW") - 2082844800 ))
  "$PY" scripts/dmg/dsstore.py "$WORK/DS_Store" "$VOL" "$CREATED" 16 17 "$NAME"
  hfsplus "$RAW" add "$WORK/DS_Store" /.DS_Store
  # finderInfo[2] (header +80+8) = root folder CNID 2: Finder opens the
  # window on mount, as `bless --openfolder` does.
  printf '\x00\x00\x00\x02' | dd of="$RAW" bs=1 seek=$((1024 + 88)) conv=notrunc status=none
  dmg build "$RAW" "$DMG" >/dev/null
fi
(cd dist && { sha256sum ./*.dmg 2>/dev/null || shasum -a 256 ./*.dmg; })
