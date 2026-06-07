---
title: 套用與產生
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 套用與產生

![routerd validate、plan、dry-run、apply 與 render 使用同一個有效資源圖的流程](/img/diagrams/concept-apply-and-render.png)

routerd 有幾個日常運作中常用的操作。
本頁統一說明文件中使用的術語。

## 驗證

`routerd validate` 確認 YAML 的格式。
可偵測 Kind 名稱、必要欄位、值範圍，以及明顯的相依性錯誤。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

## 檢視計畫

`routerd plan` 顯示準備對主機執行的操作內容。
在套用至正式路由器之前，可確認管理連線是否會中斷、是否有意外的路由變更。

```bash
routerd plan --config /usr/local/etc/routerd/router.yaml
```

## 模擬執行

`--dry-run` 在不變更主機的情況下，僅確認套用結果。
routerd 在新控制器開發與實機驗證初期，以模擬執行作為預設模式。

```bash
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## 套用

`routerd apply` 依照 YAML 的意圖變更主機。
只執行一次時加上 `--once`。
若要持續運行，請使用 `routerd serve`。

```bash
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo routerd serve --config /usr/local/etc/routerd/router.yaml
```

## 產生

文件中的「產生」，是指 routerd 組裝 dnsmasq 設定、nftables 設定、systemd 單元、NixOS 設定等面向主機的檔案。
僅完成產生並不代表主機會立即變更。
是否實際套用，取決於套用處理與模擬執行的指定方式。

目前的 routerd 中，dnsmasq 不負責 DNS 回應。
針對 dnsmasq 只產生 DHCPv4、DHCPv6、中繼、RA 的設定。
DNS 監聽、本地區域、條件式轉發、加密 DNS 由 `DNSResolver` 負責。
`DNSResolver` 是 `routerd-dns-resolver` 的執行設定。

## 調和（Reconcile）

在常駐模式下，routerd 接收事件並重新評估必要的資源。
這個「縮小意圖與現有狀態差距的處理」，在本文件中稱為調和（reconcile）。
例如，DHCPv6-PD 的 Renew 後前綴發生變化，調和會依序傳遞至 LAN 位址、RA、DNS 回應、DS-Lite 路由。
