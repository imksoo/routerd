#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/routerd-install-deps.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

assert_contains()
{
    haystack=$1
    needle=$2
    label=$3
    case "${haystack}" in
        *"${needle}"*) ;;
        *)
            echo "missing ${label}: ${needle}" >&2
            echo "${haystack}" >&2
            exit 1
            ;;
    esac
}

fakebin="${tmpdir}/bin"
mkdir -p "${fakebin}"
cat > "${fakebin}/apk" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "${fakebin}/apk"

alpine_deps=$(ROUTERD_INSTALL_PACKAGE_MANAGER=apk PATH="${fakebin}:/bin" ./packaging/install.sh --list-deps)
assert_contains "${alpine_deps}" "package manager: apk" "Alpine package manager detection"
assert_contains "${alpine_deps}" "- alpine-conf" "Alpine dependency package list"
assert_contains "${alpine_deps}" "- ppp-pppoe" "Alpine PPPoE dependency package"
assert_contains "${alpine_deps}" "- radvd" "Alpine RA dependency package"
assert_contains "${alpine_deps}" "- lbu" "Alpine live persistence command check"

config="${tmpdir}/alpine-package.yaml"
cat > "${config}" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: alpine-package-smoke
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: alpine-deps
      spec:
        packages:
          - os: alpine
            manager: apk
            names:
              - dnsmasq
              - nftables
              - conntrack-tools
EOF

# shellcheck disable=SC2016
scripts/routerd-sandbox-run.sh sh -c '
    go run ./cmd/routerctl validate --socket "${ROUTERD_SANDBOX_STATUS_SOCKET}" -f "$1" --replace >/dev/null
    go run ./cmd/routerctl apply --socket "${ROUTERD_SANDBOX_SOCKET}" -f "$1" --replace > "$2"
' sh "${config}" "${tmpdir}/status.json"

render_config="${tmpdir}/alpine-render.yaml"
cat > "${render_config}" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: alpine-render-smoke
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: eth1
        adminUp: true
        managed: true
        owner: routerd
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ip
      spec:
        interface: lan
        address: 192.168.10.1/24
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Server
      metadata:
        name: lan-dhcp
      spec:
        interface: lan
        addressPool:
          start: 192.168.10.100
          end: 192.168.10.150
        gateway: 192.168.10.1
        dnsServers:
          - 192.168.10.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        role: internet
        type: ping
        target: 1.1.1.1
        protocol: icmp
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: wan-v4
      spec:
        interface: lan
        hostname: smoke-router
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: lan
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan
      spec:
        listen:
          - addresses: [127.0.0.1]
            port: 5053
            sources:
              - default
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata:
        name: default
      spec:
        resolver: DNSResolver/lan
        match: ["."]
        upstreams:
          - DNSUpstream/cloudflare
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSUpstream
      metadata:
        name: cloudflare
      spec:
        protocol: udp
        address: 1.1.1.1
        port: 53
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallEventLog
      metadata:
        name: default
      spec:
        enabled: true
        path: /var/lib/routerd/firewall-logs.db
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: wan-pppoe
      spec:
        interface: lan
        username: user
        password: pass
    - apiVersion: net.routerd.net/v1alpha1
      kind: TailscaleNode
      metadata:
        name: edge
      spec:
        advertiseExitNode: true
EOF

render_dir="${tmpdir}/alpine-render"
go run ./cmd/routerctl render alpine --config "${render_config}" --out-dir "${render_dir}" >/dev/null
test -x "${render_dir}/openrc-routerd_healthcheck_internet"
test -x "${render_dir}/openrc-routerd_dnsmasq"
test ! -e "${render_dir}/openrc-routerd_dns_resolver_lan"
test -x "${render_dir}/openrc-routerd_firewall_logger"
test -x "${render_dir}/openrc-routerd_pppoe_client_wan_pppoe"
test -x "${render_dir}/openrc-routerd_tailscale_edge"
test -f "${render_dir}/dnsmasq.conf"
assert_contains "$(cat "${render_dir}/openrc-routerd_healthcheck_internet")" "#!/sbin/openrc-run" "OpenRC healthcheck script"
assert_contains "$(cat "${render_dir}/openrc-routerd_dnsmasq")" "--conf-file=/usr/local/etc/routerd/dnsmasq.conf" "OpenRC dnsmasq script"
assert_contains "$(cat "${render_dir}/openrc-routerd_firewall_logger")" "routerd-firewall-logger" "OpenRC firewall logger script"

echo "install dependency smoke checks passed"
