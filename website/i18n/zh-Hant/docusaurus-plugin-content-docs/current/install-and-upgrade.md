---
title: 安裝與升級
---

# 安裝與升級

![routerd 從發布封存檔安裝、檢查設定、dry-run 到啟動服務的流程](/img/diagrams/install-and-upgrade.png)

本頁是 Ubuntu Server + systemd 的入門路線。第一次安裝請在隔離 VM 或有本機主控台的備用主機上操作；不要在唯一的生產閘道上用遠端 SSH 試錯。

## 1. 下載並安裝發布封存檔

從 [GitHub Releases](https://github.com/imksoo/routerd/releases) 選擇符合 CPU 架構的封存檔。以下使用目前建議的穩定里程碑 [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514)；此建議以[穩定版頁面](./releases/stable.md)為唯一來源。範例為 Linux amd64；arm64 請改用 `linux-arm64` 檔案。

```bash
RELEASE=v20260707.1514
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

把範例複製成自己的設定，並依主機實際情況修改介面名稱。`ens18`、`ens19` 只是常見 VM 名稱，並非每一台電腦都一樣。

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudoedit /usr/local/etc/routerd/router.yaml
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
```

`routerd validate` 會直接讀取檔案，不依賴已運行的服務，也不會改動主機網路。

下一步是隔離的 dry-run。把狀態、報告和渲染目的地都導向新建的暫存目錄，避免落到系統預設路徑；`--dry-run` 本身不會提交網路變更。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json" \
  --netplan-file "$LAB_DIR/50-routerd.yaml" \
  --dnsmasq-file "$LAB_DIR/dnsmasq.conf" \
  --dnsmasq-service-file "$LAB_DIR/routerd-dnsmasq.service" \
  --nftables-file "$LAB_DIR/routerd-nat.nft"
```

`--skip-service-manager` 會略過服務管理器操作。查看輸出中的介面名稱、相依項目與警告。dry-run 可能讀取部分主機狀態以產生計畫，但不會把網路設定套用到主機；它也不能證明網線、ISP 或防火牆策略在真實流量下正確。

確定已有主控台或獨立管理路徑後，才啟動一般服務：

```bash
sudo systemctl enable --now routerd
sudo systemctl status routerd --no-pager
sudo routerctl get status
```

此時 `routerd serve` 已由 systemd 運行，`routerctl` 才有可連線的本機 socket。之後的 `routerctl validate`、`routerctl plan` 和 `routerctl apply` 都是對這個執行中服務提出請求。

## 升級

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
