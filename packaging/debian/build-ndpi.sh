#!/bin/sh
set -eu
if [ "$#" -ne 4 ]; then
    echo 'usage: build-ndpi.sh AGENT OUTPUT VERSION ARCH' >&2
    exit 2
fi
agent=$1
output=$2
version=${3#v}
arch=$4
stage=$(mktemp -d "${TMPDIR:-/tmp}/routerd-ndpi-deb.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
install -d "$stage/DEBIAN" "$stage/debian" "$stage/usr/local/sbin"
install -m 0755 "$agent" "$stage/usr/local/sbin/routerd-ndpi-agent"
cat > "$stage/debian/control" <<CONTROL
Source: routerd-ndpi
Section: net
Priority: optional
Maintainer: routerd maintainers <routerd@localhost>
Standards-Version: 4.6.0

Package: routerd-ndpi
Architecture: any
Description: libnDPI agent for routerd
CONTROL
depends=$(cd "$stage" && dpkg-shlibdeps -O usr/local/sbin/routerd-ndpi-agent)
depends=${depends#shlibs:Depends=}
cat > "$stage/DEBIAN/control" <<CONTROL
Package: routerd-ndpi
Version: $version
Architecture: $arch
Maintainer: routerd maintainers <routerd@localhost>
Depends: $depends
Description: libnDPI agent for routerd
CONTROL
cat > "$stage/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if [ "$1" = configure ] && [ -d /run/systemd/system ] && systemctl is-active --quiet routerd-ndpi-agent.service; then
    systemctl restart routerd-ndpi-agent.service
fi
POSTINST
chmod 0755 "$stage/DEBIAN/postinst"
rm -rf "$stage/debian"
dpkg-deb -Zgzip --root-owner-group --build "$stage" "$output"
