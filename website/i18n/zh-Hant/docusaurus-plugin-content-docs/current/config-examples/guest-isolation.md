---
title: 訪客與 IoT 裝置隔離
sidebar_position: 60
---

# 訪客與 IoT 裝置隔離：先理解限制

![共用 LAN 上的 ClientPolicy：受信任裝置、訪客或 IoT MAC 位址，以及管理網路](/img/diagrams/config-example-guest-isolation.png)

`ClientPolicy` 可將列出的 MAC 位址視為訪客或 IoT 裝置：允許它們連往網際網路，同時表達不應存取受信任 LAN 和管理網路的意圖。完整 YAML 位於 `examples/guest-mode.yaml`。

它適合用來學習共用 LAN 的分類方式，但**不能把共用的二層網路變成真正隔離的網路**。

## 關鍵限制：同一二層網路仍可直接通訊

如果訪客裝置和受信任裝置接在同一台交換器、同一個未分 VLAN 的 Wi-Fi SSID，或同一個乙太網路廣播域，它們的流量可能根本不會經過路由器。路由器規則看不到這種直接的二層通訊。

需要真正訪客隔離時，請使用：

- 獨立 VLAN，並讓交換器和 AP 正確標記與隔離埠；
- 對應該 VLAN 的獨立訪客 SSID；
- 或完全分開的實體埠／交換器與子網。

MAC 位址是網卡在二層網路使用的硬體識別，也可能被偽造，或因裝置的「私有 MAC」功能改變。因此 MAC 分類不適合作為高安全情境的身分驗證。

VLAN 是由交換器或無線 AP 在二層劃出的虛擬網路，不只是替同一條網線換一段 IPv4 位址。只有交換器、AP 和路由器都依同一 VLAN 設計設定，訪客流量才會被迫經過應有的三層策略點。

## ClientPolicy 的最小結構

下例假設 `lan` 已經是 `trust` 區域中的 `Interface`：

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - 18:ec:e7:33:12:6c
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

`mode: include` 表示只有列出的 MAC 位址當作訪客，其餘裝置維持原本分類。`mode: exclude` 則相反：列出的裝置信任，其餘裝置視為訪客。

可搭配 `DHCPv4Reservation` 固定裝置名稱和 IPv4 位址，讓識別較容易；但這不會解決共用二層網路繞過路由器的問題。

:::caution 不要把它當成唯一安全邊界
routerd 的防火牆與 ClientPolicy 仍在預發布的基礎實作階段。這個 Linux/nftables 範例不能取代 VLAN 設計、無線隔離、交換器設定或安全審查。要保護管理網路與可信任裝置，先做二層分段，再把路由器策略當作額外一層。
:::

## 安全檢查

先使用本機 `routerd` 檢查檔案。下列獨立檢查需要具有 `sudo` 權限的本機使用者，不需要 daemon，也不會套用網路變更：

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/guest-mode.yaml
sudo routerd apply --config examples/guest-mode.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

不應使用 `routerctl validate` 或 `routerctl plan` 代替它們。當 `routerd serve` 已在有主控台保護的真實主機上運行後，才查詢結果；首次安裝請保留 `sudo`。若管理員透過 `routerd` 群組授予本機 socket 存取權，加入後必須重新登入才會生效：

```bash
sudo routerctl get status
sudo routerctl describe ClientPolicy/guest-devices
```

這個範例對應 Linux 的 nftables 路徑。不要假定 FreeBSD 或 NixOS 會以完全相同的 MAC／二層方式執行；部署前請查看[支援的平台](../platforms.md)。
