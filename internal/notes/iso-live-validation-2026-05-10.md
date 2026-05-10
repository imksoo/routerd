# routerd live ISO validation - 2026-05-10

## Scope

- Host: pve06
- Temporary VM: 200 (`routerd-live-demo`)
- WAN bridge: `vmbr0`
- LAN bridge: temporary isolated `vmbr490`
- Client: Linux network namespace on pve06 attached to `vmbr490`

`vmbr404` was not used because router04 also uses that bridge. The validation
kept the live demo isolated from existing router VMs.

## Fixes validated

- GRUB serial console output for `qm terminal`.
- Full Alpine ISO contents are preserved instead of copying only kernel,
  initramfs, and modloop files.
- Alpine apkovl is auto-detected from ISO root.
- `/etc/.default_boot_services` is included so Alpine starts `modloop`.
- `install.sh` supports Alpine `apk` dependencies.
- `install.sh configure` prompts for an RA prefix when RA is enabled.
- Linux hosts without systemd/netplan are detected as OpenRC-style Linux.
- One-shot apply can bring a managed interface up and assign IPv4 addresses
  with `iproute2`.
- One-shot apply can start a directly managed dnsmasq process without systemd.
- Live ISO starts `routerd serve` after apply so Web Console and DNS resolver
  are immediately available.

## Observed result

- Alpine booted to login on serial console.
- WAN DHCP on `eth0` succeeded. The final run received `192.168.1.43`.
- The setup wizard generated and installed `/usr/local/etc/routerd/router.yaml`.
- `routerd validate` passed.
- `routerd apply --once` finished `Healthy`.
- LAN `eth1` became up with `192.168.10.1/24`.
- dnsmasq started and served DHCPv4 on the LAN bridge.
- nftables NAT and firewall rules were applied.
- `routerctl status` reached the live daemon and reported:
  - phase: `Healthy`
  - generation: `1`
  - resourceCount: `16`

## Client validation

The pve06 network namespace client received DHCPv4:

- address: `192.168.10.109/24`
- default route: `192.168.10.1`

Functional checks from the client namespace:

- DNS: `dig @192.168.10.1 www.google.com A +short` returned A records.
- Web Console API: `curl http://192.168.10.1:8080/api/v1/summary` returned `200`.
- IPv4 internet: `curl https://www.google.com/generate_204` returned `204`.
- ICMP: `ping -c 2 1.1.1.1` had 0% packet loss.

## Cleanup

The temporary VM, network namespace, veth pair, temporary bridge, and pvetest
ISO files were removed after validation.

## Serial console follow-up

After adding an explicit Alpine `/etc/inittab` overlay, the ISO was rebuilt as
`routerd-live-routerd-live-serialtest.iso` and booted on pve06 as temporary VM
200 with:

- `--serial0 socket`
- `--vga serial0`
- WAN `vmbr0`
- isolated LAN `vmbr490`

`qm terminal 200` reached `/dev/ttyS0` at 115200 8N1. Root login started the
same `install.sh configure` wizard over the serial console. The wizard accepted
plain text input, installed `/usr/local/etc/routerd/router.yaml`, ran
`routerd apply --once`, and reached:

- phase: `Healthy`
- generation: `1`
- resourceCount: `14`

The temporary VM, bridge, and uploaded serial-test ISO were removed after the
check.
