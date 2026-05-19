---
title: 從 NixOS 開始
---

# 從 NixOS 開始

NixOS 使用 routerd 的完整 resource model。建議做法是透過宣告式 NixOS 設定來驅動 routerd 管理的服務,不要依賴 transient systemd unit。

## 建議的起始範圍

在 NixOS 上,先從 daemon 型 WAN 服務開始。DHCPv6-PD、DHCPv4 client lease、PPPoE session、HealthCheck、dnsmasq、firewall logging、nftables 啟用,以及主 `routerd.service`,都可以寫進生成的 NixOS module。基礎服務能以 `nixos-rebuild test` 乾淨收斂後,再加入更多 router resource。

## 生成的成果物

routerd 會把 systemd units 寫入 `/etc/nixos/routerd-generated.nix`。用下列命令套用:

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成的 units 會以明確 binary path 啟動 routerd daemons,並帶有適合的 `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict` 與 capability 清單。

## 為何不用 transient units

NixOS 上放在 `/run/systemd/system` 的 unit 不是系統設定的一部分。重新開機或執行 `nixos-rebuild switch` 後會被移除。若要跨重新開機與 rebuild 保留 unit,就必須把 unit 宣告在 NixOS 設定中。routerd 透過寫入 `/etc/nixos/routerd-generated.nix` 達成這一點。

## 目前覆蓋範圍

已實作:

- `routerd-dhcpv6-client` 的 systemd unit 生成
- `routerd-dhcpv4-client` 的 systemd unit 生成
- `routerd-pppoe-client` 的 systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`generated service artifacts` 的 NixOS module 生成
- `nixos-rebuild switch` 後 DHCPv6-PD 到達 `Bound`
- dnsmasq、DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 client、DHCPv6 client、PPPoE client service 可透過生成 module 宣告
- NAT、firewall、policy routing、Path MTU resource 需要 nftables 時自動啟用
- `nixos-rebuild switch` 失敗時嘗試 `nixos-rebuild switch --rollback`
- WireGuard / Tailscale / VXLAN 已在 NixOS / Linux / FreeBSD 間確認
- VRF 以 systemd-networkd 的 native netdev 生成

各平台細節請參考 [支援的平台](../platforms.md)。

## 下一步

接著請在英文或日文文件中查看 install、first router、WAN-side services 的教學。
