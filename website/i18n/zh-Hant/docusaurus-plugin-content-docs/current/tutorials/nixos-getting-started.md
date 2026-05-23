---
title: 從 NixOS 開始
---

# 從 NixOS 開始

NixOS 是 routerd 的主要輔助平台。
在 NixOS 上，建議透過宣告式 NixOS 設定來管理 routerd 的管理服務，而非使用臨時的 systemd unit。
routerd 的執行檔從 release archive 安裝。
不過，OS 套件請透過 NixOS 設定管理。
`install.sh` 不會以 `nix-env` 安裝套件，只會輸出警告。

## 建議的起始方式

在 NixOS 上，請先以宣告式方式管理常駐程式型的 WAN 側服務。
DHCPv6-PD、DHCPv4 客戶端租約、PPPoE 連線、HealthCheck、dnsmasq、防火牆日誌記錄、nftables 啟用，以及主要的 `routerd.service`，都可以透過產生的 NixOS 模組來描述。
請先確認基礎服務能以 `nixos-rebuild test` 正常收斂，再加入其他路由器資源。

## 產生的成果物

routerd 會將 systemd unit 寫入 `/etc/nixos/routerd-generated.nix`。使用下列指令套用：

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

產生的 unit 會以明確的路徑啟動 routerd 常駐程式，
並具備適當的 `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict` 與所需的 capability。

## 為何不使用臨時 unit

在 NixOS 上，放在 `/run/systemd/system` 的 unit 不屬於系統設定的一部分。
重新開機或執行 `nixos-rebuild switch` 後就會消失。
若要讓 unit 在重新開機和重新建置後仍然保留，就必須在 NixOS 設定中宣告。
routerd 透過寫入 `/etc/nixos/routerd-generated.nix` 來實現這一點。

## 目前支援範圍

已實作的功能如下。

- `routerd-dhcpv6-client` 的 systemd unit 產生
- `routerd-dhcpv4-client` 的 systemd unit 產生
- `routerd-pppoe-client` 的 systemd unit 產生
- `Package` override、`SysctlProfile`、derived host runtime artifact、`generated service artifacts` 的 NixOS 模組產生
- `nixos-rebuild switch` 後 DHCPv6-PD 能達到 `Bound` 狀態
- dnsmasq、DNS 解析器、HealthCheck、防火牆日誌記錄器、Tailscale、DHCPv4 客戶端、DHCPv6 客戶端、PPPoE 客戶端服務可透過產生的模組宣告
- NAT、firewall、policy routing、Path MTU 資源所需的 nftables 自動啟用
- `nixos-rebuild switch` 失敗時嘗試執行 `nixos-rebuild switch --rollback`
- WireGuard / Tailscale / VXLAN 已確認可在 NixOS / Linux / FreeBSD 之間運作

各平台的詳細說明請參閱 [支援的平台](../platforms.md)。

## 相關項目

- [安裝](./install.md)
- [建立第一台路由器](./first-router.md)
- [WAN 側服務](./wan-side-services.md)
