#!/bin/sh
set -eu
if [ "$#" -ne 4 ]; then
    echo 'usage: build.sh DISTROOT OUTPUT VERSION ARCH' >&2
    exit 2
fi
distroot=$1
output=$2
version=${3#v}
arch=$4
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
stage=$(mktemp -d "${TMPDIR:-/tmp}/routerd-deb.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
install -d "$stage/DEBIAN" "$stage/usr/local/sbin" "$stage/usr/local/libexec/routerd" \
    "$stage/usr/local/etc/routerd" "$stage/usr/share/doc/routerd" "$stage/lib/systemd/system"
for binary in "$distroot"/bin/*; do
    [ "$(basename "$binary")" = routerd-ndpi-agent ] && continue
    install -m 0755 "$binary" "$stage/usr/local/sbin/"
done
cp -a "$distroot/libexec/routerd/plugins" "$stage/usr/local/libexec/routerd/"
install -m 0644 "$distroot/etc/routerd/router.yaml.sample" "$stage/usr/local/etc/routerd/"
install -m 0644 "$distroot"/systemd/*.service "$stage/lib/systemd/system/"
install -m 0644 "$distroot/share/doc/README.md" "$stage/usr/share/doc/routerd/"
install -m 0755 "$script_dir/postinst" "$script_dir/prerm" "$script_dir/postrm" "$stage/DEBIAN/"
cat > "$stage/DEBIAN/control" <<CONTROL
Package: routerd
Version: $version
Architecture: $arch
Maintainer: routerd maintainers <routerd@localhost>
Depends: adduser, ca-certificates, iproute2, nftables, dnsmasq-base
Description: Declarative router configuration and management
CONTROL
dpkg-deb -Zgzip --root-owner-group --build "$stage" "$output"
