---
title: 啟動第一台實驗路由器
sidebar_position: 2
---

# 啟動第一台實驗路由器

![第一台路由器教學：DHCPv4 WAN、LAN DHCP、NAT44、驗證、dry-run、live apply 與狀態檢查](/img/diagrams/tutorial-first-router.png)

本教學讓隔離 LAN 上的測試用戶端取得私有 IPv4 位址與閘道，並透過 IPv4 連到上游網路。
它使用完整範例
[`examples/example-basic-ipv4-nat.yaml`](../../../../../../examples/example-basic-ipv4-nat.yaml)。

這是實驗教學，不是可直接貼到家用路由器上的設定。

:::danger 保留復原路徑

請使用 VM 主控台、序列主控台或另一張管理 NIC。下方的 `ens18` 與 `ens19` 只是範例。
先用 `ip -br link` 查看真正的介面名稱，絕不要猜測哪張介面正承載管理連線。範例的
`192.168.10.0/24` 若和上游、VPN、學校或管理網路重疊，請換成未使用的私有網段。

:::

## 成功時會看到什麼

```text
上游網路 -- WAN -- routerd 主機 -- LAN -- 測試用戶端
                              DHCP + NAT
```

1. 上游透過 DHCP 將 IPv4 位址交給 WAN。
2. 路由器將 `192.168.10.100` 到 `192.168.10.199` 的位址交給 LAN 測試用戶端。
3. 測試用戶端使用 `192.168.10.1` 作為閘道。
4. NAT 讓用戶端的 IPv4 流量共用一個上游位址往外送出。

如果 DHCP、閘道或 NAT 是新詞，請先閱讀[網路基本概念](./network-basics.md)。

## 1. 取得完整的實驗檔案並調整

```bash
# 已安裝的 release 只有 router.yaml.sample，不含所有 repository 範例。
# 從與已安裝 routerd 相同的 release 取得這個範例。
ROUTERD_VERSION="$(sudo routerd version | awk '{print $2}')"
curl --fail --location --output first-router.yaml \
  "https://raw.githubusercontent.com/imksoo/routerd/${ROUTERD_VERSION}/examples/example-basic-ipv4-nat.yaml"
```

若你已有相同 release tag 的 source checkout，也可以改為複製該 checkout 的
`examples/example-basic-ipv4-nat.yaml`。

在第一次 preview 前，只調整 `first-router.yaml` 中下列值。

- `Interface/wan.spec.ifname`: 實驗 WAN 的介面名稱。
- `Interface/lan.spec.ifname`: 隔離 LAN 的介面名稱。
- `192.168.10.0/24`: 只有在它與已連接網路重疊時才修改。
- 公開 DNS 伺服器: 只有實驗必須使用其他 resolver 時才修改。

這個檔案讓 WAN 維持由外部管理，並讓 routerd 管理 LAN。它包含 DHCPv4 伺服器、NAT44
規則與基本 zone 資源。

:::caution firewall 的範圍

routerd 的 firewall 資源只是功能基礎，不是安全認證。請勿只依靠這個範例就把路由器公開到
網際網路。第一次實驗應放在隔離的虛擬交換器或實體實驗網路內。

:::

## 2. 在 daemon 存在前先驗證與 preview

```bash
sudo routerd validate --config first-router.yaml

LAB_DIR="$(mktemp -d)"
sudo routerd apply --config first-router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

這兩個命令不會變更主機網路。驗證失敗時，請修改 YAML，而不是嘗試 live apply。請確認
dry-run 顯示的管理介面、路由和將產生的檔案都符合預期。

## 3. 從主控台套用

只有當 preview 顯示預期的介面與產生物時，才從主控台或獨立管理路徑套用變更。

```bash
sudo routerd apply --config first-router.yaml --once
```

若要讓實驗路由器持續運行，請將確認過的檔案放到標準位置並啟動服務。

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
```

## 4. 完整檢查這條小路徑

在路由器上執行：

```bash
sudo routerctl get status
sudo routerctl describe DHCPv4Client/wan-dhcpv4
sudo routerctl describe DHCPv4Server/lan-dhcpv4
sudo routerctl describe NAT44Rule/lan-to-wan
```

在只連到隔離 LAN 的用戶端更新 DHCP 租約，再執行：

```bash
ip route
ping 192.168.10.1
curl -I https://example.com/
```

如果最後一個命令失敗，請分開找原因：先確認用戶端是否取得位址與閘道，再確認 WAN 是否有
租約，最後才檢查 DNS。原因不明時，不要重複套用檔案。

## 下一步

- [基本 IPv4 NAT 閘道](../config-examples/basic-ipv4-nat.md) — 以圖和 YAML 對照說明這個檔案
- [LAN 側服務](./lan-side-services.md) — IPv4 正常後再加入本地 DNS 與 IPv6
- [基本 NAT 與 firewall policy](./basic-firewall.md) — 目前 firewall 的範圍與較安全的下一步
