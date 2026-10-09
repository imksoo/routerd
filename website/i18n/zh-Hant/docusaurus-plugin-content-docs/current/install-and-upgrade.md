---
title: 安裝與升級
---

# 安裝與升級

![routerd 從發布封存檔安裝、檢查設定、dry-run 到啟動服務的流程](/img/diagrams/install-and-upgrade.png)

本頁是 Ubuntu Server + systemd 的入門路線。第一次安裝請在隔離 VM 或有本機主控台的備用主機上操作；不要在唯一的生產閘道上用遠端 SSH 試錯。

## 1. 下載並安裝發布封存檔

從 [GitHub Releases](https://github.com/imksoo/routerd/releases) 選擇符合 CPU 架構的封存檔。以下使用目前建議的穩定里程碑 [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729)；此建議以[穩定版頁面](./releases/stable.md)為唯一來源。範例為 Linux amd64；arm64 請改用 `linux-arm64` 檔案。

```bash
RELEASE=v20260914.0729
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

封存檔包含執行檔、服務範本、設定範例與安裝腳本；路由器主機不需要 Go 或 Makefile。安裝腳本會安裝或確認常用執行期相依套件，並將程式放到 `/usr/local/sbin`。它會建立 `/usr/local/etc/routerd/router.yaml.sample`，不會覆寫既有的 `/usr/local/etc/routerd/router.yaml`。

先確認程式可用：

```bash
routerd --version
routerd --help
```

## 2. 先準備設定，之後才啟動服務

依[安全起步](./tutorials/getting-started.md)完成離線檢查，再依
[第一台實驗路由器](./tutorials/first-router.md)準備完整 DHCP/NAT 設定、啟動服務、
檢查狀態並測試用戶端。`router.yaml.sample` 是包含 ISP 特定設定的較大範例，
不能只改介面名稱就當成通用預設值。一般全新 `./install.sh` 只安裝檔案，不會啟動服務。

## 升級

升級會保留目前 routerd 產生的 `routerd.service`，包括設定中的 Capability 和 Environment。
`Managed by routerd` 註解本身不代表舊格式；含已移除的 `--controller-chain` 參數的 unit 會被替換。
執行期設定世代切換會保留設定未變的受管理 DHCP 用戶端。
VRRP `gracefulActivation` 控制 VIP 的初次發布；相同位址、介面已有 Ready/advertised MASTER
紀錄時，暫時的 readiness 下降不會撤銷 VIP。只有位址存在而沒有發布紀錄時仍須通過初始檢查。
降為 BACKUP/FAULT 時仍會撤銷 VIP。
成功發布歷史與目前觀測狀態分開保存，觀測錯誤不會清除歷史。
降級、確認 VIP 不存在或發布失敗會使歷史失效；不同位址或介面不能沿用歷史。

下載新版本、核對雜湊、解壓縮後，再執行相同安裝腳本：

```bash
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

安裝程式會保留既有設定與狀態。升級前請備份自己的 `router.yaml`；若 `routerd.service` 已在執行，安裝程式可能會重新啟動它以換用新版程式。因此請先安排維護時段並確認管理路徑；升級後檢查服務狀態和本機控制介面：

```bash
sudo systemctl is-active routerd.service
sudo routerctl get status
```

若生產環境使用 BGP 等對短暫重啟敏感的功能，請先閱讀[變更記錄](./releases/changelog.md)與自己的維運流程。

## 平台範圍

Ubuntu Server 是目前主要目標。FreeBSD 與 NixOS 的安裝配置、服務管理和網路產生器仍是各自的平台工作，不能把本頁的 systemd/nftables 指令視為完全等價的操作說明。請先閱讀[支援的平台](./platforms.md)。

## 繼續學習

- [安全起步](./tutorials/getting-started.md)
- [第一台實驗路由器](./tutorials/first-router.md)
- [發布版與穩定版](./releases/stable.md)
