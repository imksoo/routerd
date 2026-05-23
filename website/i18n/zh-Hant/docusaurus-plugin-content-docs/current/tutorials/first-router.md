---
title: 啟動第一台路由器
sidebar_position: 2
---

# 啟動第一台路由器

本教學將啟動最小的 routerd 構成。構成為「透過 DHCPv4 取得 IPv4 的 WAN 一條」加上「固定 IPv4 位址的 LAN 一條」。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: wan
      spec:
        interface: wan

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-address
      spec:
        interface: lan
        address: 192.0.2.1/24
```

`DHCPv4Client` 由 `routerd-dhcpv4-client` 擁有。
routerd 不依賴 OS 內建的用戶端。這個常駐程式與其他 routerd 常駐程式使用相同的約定（`/v1/status`、`lease.json`、`events.jsonl`）來公開狀態。

套用至生產環境前，請先以 validate 和 plan 確認。

```bash
routerd validate --config first-router.yaml
routerd plan     --config first-router.yaml
routerd apply    --config first-router.yaml --once --dry-run
```

確認管理路徑（透過 LAN 的 SSH、主控台、Hypervisor 主控台）在變更後仍可存取，再去掉 `--dry-run` 正式套用。

## 接下來閱讀

- [WAN 側服務](./wan-side-services.md) — DHCPv6-PD、PPPoE、DS-Lite
- [LAN 側服務](./lan-side-services.md) — DHCP、RA、DNS、本地區域
- [基本的 NAT 與防火牆政策](./basic-firewall.md)
