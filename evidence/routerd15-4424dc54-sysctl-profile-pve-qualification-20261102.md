# routerd 1.5 current Linux PVE SysctlProfile withdrawal qualification

## Scope

This record qualifies Linux `SysctlProfile` retirement in the binary built
from source revision `4424dc5478850025512c37a82b1d0e0beb38aec5`. The deployed
binary SHA-256 was
`65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`.

The isolated PVE router VM219 used the standalone 21-resource routerd 1.5
configuration. Its existing NAT44-derived `SysctlProfile/router-runtime`
continued to own IPv6 forwarding.

## Observed result

- An explicit temporary `SysctlProfile` using `router-linux`, with runtime
  disabled and persistence enabled, created 26 resource-owned files below
  `/etc/sysctl.d`.
- The temporary profile survived a routerd service restart while VM219
  remained `Healthy` with 22 configuration resources.
- Replacing the configuration with the 21-resource baseline removed all 26
  temporary files and the retired profile state. It did not remove or override
  the active derived `router-runtime` profile.
- The final VM219 status was `Healthy` with 21 resources and
  `net.ipv6.conf.all.forwarding=1`.

## Unverified scope

This check does not qualify host reboot persistence, profile conflict with a
different persistent owner, high availability, target-device qualification, or
FreeBSD behavior.
