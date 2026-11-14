# PPPoESession

[Resource configuration reference](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Run a supervised routerd-pppoe-client using pppd on Linux or mpd5 on FreeBSD, and publish negotiated IPv4 session status. Several legacy fields affect only manual FreeBSD render output, not this daemon path.

## Conditions and references

- interface must reference a base Interface declared in the same Router; PPPoESession, tunnels and other logical interfaces cannot be used as the lower interface. username and exactly one of password/passwordFile remain required even with enabled:false.
- The live supervisor passes authMethod, mtu/mru, serviceName/acName and lcpEchoInterval/lcpEchoFailure to the daemon. It does not pass ifname, managed, persist, ipv6, usePeerDNS, lcpInterval or lcpFailure. Do not infer live behavior from the separate manual FreeBSD render artifact.
- The live daemon defaults to MTU/MRU 1454 and LCP echo 30s/4 on both platforms. The separate manual FreeBSD mpd5 rendering defaults to 1492 and legacy LCP 20s/3. Neither the configured plan nor Connected status proves real upstream reachability.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # optional; string
    authMethod: "chap" # optional; string; enum=["chap", "pap", "both"]
    defaultRoute: true # optional; boolean
    enabled: true # optional; boolean
    ifname: "<string>" # optional; string
    interface: "<string>" # Required; string
    ipv6: true # optional; boolean
    lcpEchoFailure: 0 # optional; integer; minimum=0
    lcpEchoInterval: 0 # optional; integer; minimum=0
    lcpFailure: 0 # optional; integer; minimum=0
    lcpInterval: 0 # optional; integer; minimum=0
    managed: true # optional; boolean
    mru: 576 # optional; integer; minimum=576; maximum=1500
    mtu: 576 # optional; integer; minimum=576; maximum=1500
    password: "<string>" # optional; string
    passwordFile: "<string>" # optional; string
    persist: true # optional; boolean
    secretEncoding: "plain" # optional; string; enum=["plain"]
    serviceName: "<string>" # optional; string
    usePeerDNS: true # optional; boolean
    username: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interface` | Required lower base Interface metadata.name, resolved to its physical OS name. No status-value source. |
| `spec.ifname` | Optional desired PPP name, <=15 characters without whitespace/slash; omission requires ppp-<name> to fit 15 characters. The live supervisor currently ignores this field and the daemon uses DefaultIfName; manual FreeBSD render uses this value. Avoid relying on a custom name in the live path. |
| `spec.enabled` | Default true. False suppresses startup, stops the supervised session and reports Disabled; it does not waive configuration validation. |
| `spec.authMethod` | chap by default; pap or both also accepted. Controls permitted PAP/CHAP authentication in the live pppd/mpd5 configuration. |
| `spec.username` | Required PPP authentication username, with no derived default. |
| `spec.password` | Inline plain-text PPP password, required unless passwordFile is supplied; mutually exclusive with passwordFile. |
| `spec.passwordFile` | Router-local raw password file; required unless password is supplied. Daemon reads it and trims surrounding whitespace at startup; missing/unreadable/empty fails. Not an environment assignment file. |
| `spec.mtu` | Optional 576–1500; omitted/0 defaults to 1454 in the live daemon, 1492 in manual FreeBSD render. A 1500-byte PPP MTU requires suitable underlay support; validation does not prove it. |
| `spec.mru` | Optional 576–1500 receive limit; omitted/0 defaults to 1454 in the live daemon, 1492 in manual FreeBSD render. |
| `spec.serviceName` | Optional PPPoE service selector; empty sends no explicit service-name setting. Newlines are rejected. |
| `spec.acName` | Optional access-concentrator selector; empty sends no explicit AC-name setting. Newlines are rejected. |
| `spec.defaultRoute` | Default false. The live main-route cleanup preserves an existing default route on the observed PPP device when true, but the daemon configuration does not create that route. Manual FreeBSD render emits a default-route directive when true. Use an explicit IPv4Route if automatic installation is required in the live path. |
| `spec.usePeerDNS` | Default false but not consulted by the live daemon path. LinuxPeer always emits usepeerdns and negotiated DNS is published in PPP status; this field does not control routerd DNS forwarding or resolv.conf application. |
| `spec.persist` | Optional boolean decoded but not used by the current supervised daemon configuration; omission does not establish a resource-specific retry policy. Supervisor lifecycle is separate. |
| `spec.lcpInterval` | Legacy nonnegative interval used only by manual FreeBSD render, with 0/omitted →20 seconds. The live daemon ignores it; use lcpEchoInterval. |
| `spec.lcpFailure` | Legacy nonnegative failure multiplier used only by manual FreeBSD render, with 0/omitted →3. The live daemon ignores it; use lcpEchoFailure. |
| `spec.lcpEchoInterval` | Nonnegative LCP echo interval in seconds; omitted/0 results in the live daemon default 30s, not disabled echoes. Manual FreeBSD render ignores this field. |
| `spec.lcpEchoFailure` | Nonnegative LCP echo failure count; omitted/0 results in live default 4. Linux passes lcp-echo-failure; FreeBSD daemon renders keep-alive interval and interval×count. |
| `spec.ipv6` | Default false; currently not passed to the supervised PPP daemon and does not emit IPv6 negotiation options in its generated configuration. Do not rely on this flag to enable IPv6 over PPP. |
| `spec.managed` | Default false. The live supervisor starts any enabled session regardless of this flag. In manual FreeBSD render only, true adds an enabled session to the mpd5 default load list. It is not observe-only protection for the live path. |
| `spec.secretEncoding` | Only omitted or plain is accepted. It does not decode base64 or encrypt password/passwordFile. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go) · [pkg/controller/pppoesession/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/pppoesession/controller.go) · [cmd/routerd-pppoe-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-pppoe-client/main.go) · [pkg/pppoeclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pppoeclient/client.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [cmd/routerd/cmd_render.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_render.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `device`, `currentAddress`, `peerAddress`, `gateway`, `dnsServers`, `connectedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
