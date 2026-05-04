# Path MTU and TCP MSS

`PathMTUPolicy` manages the MTU assumptions that routerd uses for router
advertisements and TCP MSS clamping.

For tunnel paths, static MTU values are fragile. A DS-Lite tunnel, PPPoE
session, or overlay can change the usable packet size. `mtu.source: probe`
lets routerd measure the path and regenerate the nftables MSS clamp table from
the measured value.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PathMTUPolicy
metadata:
  name: lan-to-dslite-mtu
spec:
  fromInterface: lan
  toInterfaces:
    - ds-lite-a
    - ds-lite-b
    - ds-lite-c
  mtu:
    source: probe
    value: 1454
    probe:
      family: ipv4
      targets:
        - 1.1.1.1
        - 8.8.8.8
      min: 1280
      max: 1500
      fallback: 1454
      interval: 10m
      timeout: 1s
  interfaceMTU:
    enabled: true
  tcpMSSClamp:
    enabled: true
    families:
      - ipv4
```

The probe uses DF-enabled `ping` on Linux. routerd tests each destination
interface and uses the smallest successful MTU. If all probes fail, routerd uses
`fallback`.

The measured MTU is cached for `interval`. This avoids turning every controller
adjustment into an active network probe.

When `interfaceMTU.enabled` is true, routerd also lowers the destination
interfaces to the measured MTU. This is useful for tunnel interfaces where UDP
or non-TCP traffic should see the same packet-size limit as TCP.

For IPv4 TCP, routerd sets MSS to `MTU - 40`. For IPv6 TCP, routerd sets MSS to
`MTU - 60`.
