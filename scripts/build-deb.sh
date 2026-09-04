#!/bin/sh
# Build the .deb without root: pure-Go binary + static packaging sources.
# usage: sh scripts/build-deb.sh   (output: out/mwb-client_<ver>_amd64.deb)
set -eu
cd "$(dirname "$0")/.."

VER=$(sed -n 's/^const Version = "v\([^"]*\)".*/\1/p' cmd/mwb-client/main.go | head -1)
if [ -z "$VER" ]; then
  echo "build-deb: cannot parse Version from cmd/mwb-client/main.go" >&2
  exit 1
fi

WORK=out/debroot
rm -rf "$WORK"
mkdir -p "$WORK/usr/local/bin" "$WORK/DEBIAN" "$WORK/lib/udev/rules.d"

CGO_ENABLED=0 go build -o "$WORK/usr/local/bin/mwb-client" ./cmd/mwb-client
cp packaging/deb/DEBIAN/control "$WORK/DEBIAN/control"
cp packaging/deb/DEBIAN/postinst "$WORK/DEBIAN/postinst"
chmod 755 "$WORK/DEBIAN/postinst"
cp packaging/deb/lib/udev/rules.d/99-mwb-client-input.rules "$WORK/lib/udev/rules.d/"

# Sync control version with the binary Version const.
sed -i "s/^Version: .*/Version: $VER/" "$WORK/DEBIAN/control"

mkdir -p out
dpkg-deb --root-owner-group --build "$WORK" "out/mwb-client_${VER}_amd64.deb"
echo "built out/mwb-client_${VER}_amd64.deb"
dpkg-deb -c "out/mwb-client_${VER}_amd64.deb" | head -12
