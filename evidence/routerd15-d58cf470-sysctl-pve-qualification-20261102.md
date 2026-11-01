# routerd 1.5 current Linux PVE Sysctl withdrawal qualification

## Scope

This record qualifies removal of a Linux `Sysctl` resource in the binary built
from source revision `d58cf470c09d347efd20c42190204af50310df28`. The deployed
binary SHA-256 was
`cc29175bf4dfce09b9f109a3eb1c8d4c7391240ce780a212710cee882e6956e3`.

The isolated PVE router VM219 started with the standalone 21-resource routerd
1.5 configuration. No routerd 1.0 configuration, state database, or migration
path participated in this check.

## Observed result

- The check added one temporary `Sysctl` resource for
  `net.ipv6.conf.all.forwarding` with runtime value `0` and a persistent
  configuration file. The previous runtime value was `1`.
- The resource applied successfully and survived a routerd service restart.
  VM219 remained `Healthy` with 22 resources while the temporary resource was
  present.
- Replacing the configuration with the 21-resource baseline removed the
  temporary resource. The controller restored the saved runtime value `1` and
  removed only the persistent file owned by that resource.
- The test then set IPv6 forwarding to `0` manually. This cleared the value
  left by the earlier test run before the retirement fix existed. The final
  router status was `Healthy` with 21 resources.

## Unverified scope

This check does not qualify persistent settings across a host reboot, shared
key conflict with a `SysctlProfile`, high availability, target-device
qualification, or FreeBSD behavior.
