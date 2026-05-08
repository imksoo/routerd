# DS-Lite multi-stage fallback validation, 2026-05-08

Target: router04 on pve06, FreeBSD.

Config under test:

- `ds-lite-a`: `gif41`, weight 120, source `192.0.0.2/29`
- `ds-lite-b`: `gif42`, weight 120, source `192.0.0.3/29`
- `ds-lite-c`: `gif43`, weight 120, source `192.0.0.4/29`
- `ds-lite-ra`: `gif44`, weight 80, source `192.0.0.5/29`, outer address from RA/SLAAC
- `pppoe-flets`: `ppp-flets`, weight 60, SoftEther `open@open.ad.jp`
- `hgw-direct`: `vtnet0` via `192.168.1.1`, weight 40

PPPoE physical path:

- router04 VM 131 is on pve06.
- `net0` is `vmbr0`, MAC `bc:24:11:fb:92:8d`, mapped to FreeBSD `vtnet0`.
- The original brief expected `net0=svnet2`, but live PPPoE succeeds on `vtnet0`.
- `mpd5` established `ppp-flets` with `100.64.4.249 --> 202.222.12.149`.

One issue was found before the final run:

- `PPPoEInterface` and `DHCPv4Address` did not have resource status.
- Their health checks were Healthy, but `EgressRoutePolicy` rejected them because `SourceReady` was false.
- The controller now treats a Healthy candidate health check plus a resolved output device as ready when the source resource has no status-bearing phase.

Final staged fault test:

| Stage | Forced down | Selected candidate | Device | NAT egress | curl result | tcpdump observation |
| --- | --- | --- | --- | --- | --- | --- |
| baseline | none | `ds-lite-a` | `gif41` | `gif41` | `200`, 1 MiB, 2.55 s | `192.0.0.2 -> 162.159.140.220` |
| a down | `gif41` | `ds-lite-b` | `gif42` | `gif42` | `200`, 1 MiB, 0.93 s | `192.0.0.3 -> 162.159.140.220` |
| a+b down | `gif41,gif42` | `ds-lite-c` | `gif43` | `gif43` | `200`, 1 MiB, 10.30 s | `192.0.0.4 -> 162.159.140.220` |
| a+b+c down | `gif41,gif42,gif43` | `ds-lite-ra` | `gif44` | `gif44` | `200`, 1 MiB, 1.94 s | `192.0.0.5 -> 162.159.140.220` |
| a+b+c+ra down | `gif41,gif42,gif43,gif44` | `pppoe-flets` | `ppp-flets` | `ppp-flets` | `200`, 1 MiB, 0.11 s | `100.64.4.249 -> 162.159.140.220` |
| all above down | plus `mpd5 stop` | `hgw-direct` | `vtnet0` | `vtnet0` | `200`, 1 MiB, 0.38 s | `192.168.1.35 -> 162.159.140.220` |

Notes:

- `ifconfig ppp-flets down` alone was not enough to simulate PPPoE failure because `mpd5` brought it back.
- The final PPPoE fault used `service mpd5 stop`; it was restarted after the test.
- Final restore state: router04 `Healthy`, generation 1075, default route back on `gif41`, `mpd5` running, all six health checks Healthy.
- `pfctl -nf /var/run/routerd/nat44.pf` passed after the test.
