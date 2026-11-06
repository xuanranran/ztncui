#!/bin/bash
set -euo pipefail
binary=$(realpath "${1:?binary path required}")
version=${2:?version required}
arch=${3:?architecture required}
out=${4:?output directory required}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
case "$arch" in amd64) machine=62;; arm64) machine=183;; *) exit 2;; esac
python3 - "$binary" "$machine" <<'PY'
import struct, sys
with open(sys.argv[1], 'rb') as f:
    header = f.read(20)
assert header[:4] == b'\x7fELF' and header[4:6] == b'\x02\x01', 'Expected 64-bit little-endian ELF'
assert struct.unpack('<H', header[18:20])[0] == int(sys.argv[2]), 'ELF architecture does not match DEB architecture'
PY
here=$(cd -- "$(dirname -- "$0")" && pwd)
console=$(dirname "$here")
mkdir -p "$out"
out=$(cd "$out" && pwd)
root=$(mktemp -d)
trap 'rm -rf -- "$root"' EXIT
chmod 0755 "$root"
mkdir -p "$root/DEBIAN" "$root/opt/ztncui-console" "$root/usr/lib/systemd/system" "$root/etc/ztncui-console" "$root/usr/share/doc/ztncui-console/licenses"
install -m 0755 "$binary" "$root/opt/ztncui-console/ztncui-console"
install -m 0644 "$here/debian/ztncui-console.service" "$root/usr/lib/systemd/system/"
install -m 0644 "$here/debian/console.conf" "$root/etc/ztncui-console/"
install -m 0755 "$here/debian/postinst" "$here/debian/prerm" "$here/debian/postrm" "$root/DEBIAN/"
printf '/etc/ztncui-console/console.conf\n' > "$root/DEBIAN/conffiles"
cp "$console/README.md" "$root/usr/share/doc/ztncui-console/"
cp "$console/../LICENSE" "$root/usr/share/doc/ztncui-console/copyright"
cp -R "$console/web/public/licenses/." "$root/usr/share/doc/ztncui-console/licenses/"
cp -R "$console/web/src/vendor" "$root/usr/share/doc/ztncui-console/vendor"
size=$(du -sk "$root" | cut -f1)
sed -e "s/@VERSION@/$version/g" -e "s/@ARCH@/$arch/g" -e "s/@SIZE@/$size/g" "$here/debian/control.in" > "$root/DEBIAN/control"
dpkg-deb --root-owner-group --build "$root" "$out/ztncui-console_${version}_${arch}.deb"
(cd "$out" && sha256sum "ztncui-console_${version}_${arch}.deb" > "ztncui-console_${version}_${arch}.deb.sha256")
