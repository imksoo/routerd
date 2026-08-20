---
title: 使用 routerd 前的網路基礎
sidebar_position: 0
---

# 使用 routerd 前的網路基礎

在試用 routerd 前，不必先背完所有網路術語。本頁只說明第一個教學需要的最小概念。

## 先記住這張圖

```text
Internet / 學校 / 電信業者網路
                |
               WAN
                |
          [ routerd 主機 ]
                |
               LAN
                |
       電腦、手機、遊戲主機
```

- **WAN** 是連向電信業者、學校或上游路由器的一側。
- **LAN** 是你自己管理的一側，例如家庭、教室或測試網路。
- **路由器** 在 WAN 和 LAN 之間轉送封包；它不是 Wi-Fi AP，也不是 Ethernet switch。

## 六個先會用到的詞

| 名詞 | 白話說明 |
| --- | --- |
| IP 位址 | 裝置在網路上的地址，像寄送地址。 |
| Gateway | 裝置要前往本 LAN 以外時，先交給的設備；小型網路通常就是路由器。 |
| DHCP | 自動發給裝置 IP 位址、gateway，以及常見 DNS server 的服務。 |
| DNS | 把 `example.com` 這類名稱換成 IP 位址的電話簿。 |
| NAT | 讓多台 LAN 裝置共用一條對外 IPv4 連線的方法。 |
| `/24` | 表示 `192.168.10.1` 到 `192.168.10.254` 這類一小段 IPv4 LAN 的簡寫。 |

routerd 把這些選擇寫成 YAML。`Router` 檔案是一個裝有 **resource（元件）**
清單的盒子。每個 resource 有 `kind`（種類）、`metadata.name`（你取的標籤）與
`spec`（詳細資料）。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: my-lab-router
spec:
  resources:
    # 在這裡列出 Interface、DHCPv4Server、NAT44Rule 等元件。
```

不需要死背 resource 名稱。教學會一次加入一個工作，並在需要時連到參考文件。

## 安全地開始

第一次實驗請使用隔離的 Ubuntu Server VM 或備用電腦。保留 Proxmox/VM console、
serial console，或獨立的 management NIC。不要一開始就更動承載家中、學校或工作網路的
唯一一台路由器。

`routerd validate` 與 `routerd apply --once --dry-run` 用來確認設定檔而不提交網路變更。
`routerd apply --once` 與 `routerd serve` 可能會變更主機網路。只有在有 console 或獨立
管理路徑時才執行它們。

:::caution Firewall 邊界
routerd 是 pre-release 軟體。firewall resource 仍屬 groundwork，並非安全認證。
不要把一份範例當成 Internet-facing router 的唯一安全邊界。
:::

## 下一步

1. 在隔離的 Ubuntu Server VM 上[安裝 routerd](../install-and-upgrade.md)。
2. [執行安全的第一次檢查](./getting-started.md)，不變更網路。
3. 有 console 路徑後，再[建立第一台 IPv4 實驗路由器](./first-router.md)。
