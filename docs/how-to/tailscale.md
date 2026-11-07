---
title: Tailscale exit node and subnet router
---

# Tailscale exit node and subnet router

![Diagram showing TailscaleNode installing or using tailscaled, declaring auth key files, advertised subnets, exit-node intent, and tailnet approval flow](/img/diagrams/how-to-tailscale.png)

## Scenario

Use `TailscaleNode` when a routerd host should join a tailnet and advertise:

- an exit node (`0.0.0.0/0` and `::/0`)
- one or more routed subnets
- both at the same time

routerd does not replace `tailscaled`. It generates and manages a systemd unit
that runs `tailscale up` with the declared node options. This keeps the
Tailscale account, control plane, and route approval workflow in Tailscale,
while routerd owns the host-local intent.

## Configuration changes

routerd applies the complete declared options with `tailscale up --reset`.
Removing `advertiseRoutes` or setting `advertiseExitNode: false` therefore clears
previous advertisements instead of failing Tailscale's missing-flags check.
Omitted settings return to Tailscale's defaults; declare settings such as
`acceptDNS: false` whenever they must remain disabled. Do not depend on manual
`tailscale up` preferences that are absent from the YAML. This does not log out
the node or force reauthentication. Route approval remains with the tailnet
administrator.

## Install tailscale

Declare the OS package so the dependency is visible in the router config.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - tailscale
        - tailscale-archive-keyring
    - os: nixos
      manager: nix
      names:
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - tailscale
      optional: true
```

On Ubuntu, the Tailscale apt repository must already be available before
`Package` can install `tailscale`. Use your normal bootstrap method for that
repository.

## Fresh Linux package installation

The Linux package uses `/etc/routerd/router.yaml`; the paths below follow that
layout. A fresh OS has no Tailscale node identity, even when the replacement
router uses the old hostname. Do not copy the old routerd database or assume
that a configuration with no authentication fields can join the tailnet.

Before the first routerd start:

1. Configure the Tailscale APT repository for the installed Ubuntu release and
   update its package index. Confirm
   `apt-cache policy tailscale tailscale-archive-keyring` offers installable candidates, then install those
   packages through APT. Check `systemctl is-active tailscaled.service`.
2. Provision a new-node auth key in the root-only environment file below and
   declare its path in `TailscaleNode`. Keep the key out of YAML, shell history,
   version control, and command output. A device-approval policy may require an
   additional tailnet administrator action.
3. Declare the required forwarding sysctls and firewall policy in the prepared
   router configuration. For dual-stack subnet/exit routing, verify
   `net.ipv4.ip_forward=1` and `net.ipv6.conf.all.forwarding=1` after apply.
   A running Tailscale backend alone does not prove packet forwarding.
4. Validate the complete 1.5 configuration before starting routerd. Approve the
   **new node's** advertised routes and exit-node use in the control plane;
   approval of the replaced node does not establish approval of this identity.
5. From an independent tailnet client, select the new exit node or approved
   subnet route and fetch an actual service payload over each required address
   family. Restart routerd and repeat. Require `BackendState: Running`, usable
   routes, and successful client traffic; `NeedsLogin`, `NeedsMachineAuth`, or
   status alone is not a successful deployment.

Keep the old router isolated during replacement. An existing logged-in test
fixture only qualifies reconfiguration, not this fresh-node bootstrap path.

## Authenticate without committing secrets

For production configs, prefer `authKeyEnv` plus `authKeyFile`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
  authKeyEnv: TS_AUTHKEY
  authKeyFile: /etc/routerd/secrets/tailscale.env
```

The environment file is outside the routerd YAML:

```sh
sudo install -d -m 0700 /etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /etc/routerd/secrets/tailscale.env
```

If the node is already logged in, omit `authKey`, `authKeyEnv`, and
`authKeyFile`. routerd will reapply the advertised node options without
embedding a secret in the service unit.

Tailscale listens on UDP/41641 by default. routerd reserves that port when a
`TailscaleNode` is present and validation rejects any `WireGuardInterface` that
tries to reuse it.

## Advertise private subnets

Advertising all RFC 1918 private address space is useful when the router should
be the tailnet path back into home or site networks:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
```

After applying this config, approve the advertised routes in the Tailscale
admin console. Until approval, `tailscale debug prefs` shows the requested
routes, but `tailscale status --self --json` may not include them in
`Self.AllowedIPs`.

## Firewall zone placement

Declare `tailscale0` as an `Interface` so it appears in status and in the Web
Console.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  mtu: 1280
  managed: false
```

`mtu: 1280` lets derived TCP MSS clamp rules account for the Tailscale source
path without lowering unrelated LAN-to-WAN paths.

For a home router, place `tailscale0` in the `trust` zone rather than the
`mgmt` zone:

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: lan
spec:
  role: trust
  interfaces:
    - Interface/lan
    - Interface/tailscale

---

apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: management
spec:
  role: mgmt
  interfaces:
    - Interface/mgmt
```

This allows tailnet clients to reach services on the router itself, such as the
routerd Web Console, through the normal `trust -> self` path. It does not grant
the tailnet broad access to the management VLAN if the firewall policy still
denies `trust -> mgmt` forwarding.

Use `mgmt` only when the tailnet should be treated as a full management
network.

## Apply and verify

Apply the config:

```sh
routerctl validate -f /etc/routerd/router.yaml --replace
systemctl restart routerd.service
```

Check the generated unit:

```sh
systemctl cat routerd-tailscale-edge.service
```

Check Tailscale state:

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

Check routerd state:

```sh
routerctl get status -o json
routerctl get TailscaleNode/edge -o yaml
routerctl vpn tailscale peers
```

`routerctl vpn tailscale peers -o json` reads `tailscale status --json` and formats
the peer list through routerd's CLI. The Web Console also shows the current
peer list on the `TailscaleNode` resource, including online state, relay, last
seen time, and allowed routes.

If the Web Console should be reachable over Tailscale, test it through the
router's Tailscale address or through an approved routed address:

```sh
curl -f http://100.64.0.1:8080/
```

Replace the address with the actual Tailscale IP of the router.

## Notes

- `acceptDNS: false` keeps Tailscale from replacing the router's local DNS
  resolver configuration. routerd's default model is "LAN DNS first":
  `DNSResolver`, local zones, DHCP-derived records, and conditional forwarding
  stay authoritative for LAN clients. Tailscale MagicDNS remains useful from
  tailnet clients, but it should not take over the router's host resolver.
- `acceptRoutes: false` keeps the router from importing other peers' advertised
  routes. This is common for a router that advertises routes outward.
- routerd exports Tailscale peer gauges as `routerd.tailscale.peer.count` and
  `routerd.tailscale.last_handshake.seconds`. Tailscale status exposes peer
  `LastSeen`, so routerd uses that timestamp as the operational handshake-age
  signal.
- Exit-node and subnet-route approval happens in Tailscale, not in routerd.
- Keep auth keys out of examples and Git history. Use `authKeyFile` for local
  deployments.

## Disconnect and withdraw

On Linux, removing a `TailscaleNode`, setting `state: absent`, or making its
`when` condition false stops the managed unit and runs `tailscale down`.
The disconnect uses `ExecStopPost`, so it also runs when `tailscale up` accepted
connection intent but failed before the oneshot unit reached its active state.
This disconnects the node without logging out or stopping `tailscaled`; its
identity remains available for reactivation. A failed stop leaves the unit
and cleanup error in place for reconciliation to retry.

Verify withdrawal with `tailscale debug prefs` (`WantRunning: false`) and
`tailscale status --json` (`BackendState: Stopped`), as well as the absence
of the managed unit. Empty routerd resource status alone is insufficient.

For an isolated interoperability check, use an independent tailnet peer and an
SSH server behind the router. Approve the advertised subnet at the control
plane, enable route acceptance on the peer, and check `ip route get <server-IP>`
selects `tailscale0`. A bounded `nc -w 5 <server-IP> 22` must return an SSH banner;
a Tailscale ping alone does not prove subnet forwarding. Repeat after
`state: absent` and reactivation without an auth key, checking the node keeps
its Tailscale address. Finally remove the resource and check the stopped state
above. Run these steps on disposable hosts, not a production routing path.

## Runtime status

A successfully installed unit does not prove that Tailscale is running. The
Linux service controller reports `Running` only for backend state `Running`.
`NeedsLogin` reports `Pending/AuthenticationRequired`; renew the node's
authentication using the configured tailnet procedure. `NeedsMachineAuth`
reports `Pending/MachineApprovalRequired`; approve the node in the control
server. Other non-running states report `Pending/BackendNotRunning`. A failed
or malformed CLI observation reports `Error/RuntimeObservationFailed` with
`runtimeError`. Recovery clears the prior reason/error on the next observation.

The service controller observes runtime state during reconciliation (including
its existing five-minute periodic pass). `online: false` while the backend is
Running does not by itself prove a forwarding outage; cached peer paths may
continue to work during control-server unavailability. These observations do
not rotate credentials or force reauthentication.
