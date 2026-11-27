---
title: nDPI agent native package
---

# nDPI 代理程式原生套件

routerd 的 Linux 執行檔以 `CGO_ENABLED=0` 建置並靜態連結。選用的 `routerd-ndpi` Debian 套件是為需要原生 nDPI 分類的主機提供的例外。其中只包含 `/usr/local/sbin/routerd-ndpi-agent`，以 `CGO_ENABLED=1 -tags libndpi` 建置並連結系統的 `libndpi`。`routerd` 套件不包含 nDPI 代理程式。

## 安裝

與 `routerd` 套件一起安裝。對應的 `libndpi` 執行時期由 apt 依套件相依關係安裝。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb.sha256
sha256sum -c routerd-ndpi-linux-amd64.deb.sha256
sudo apt install ./routerd-ndpi-linux-amd64.deb
```

更新套件時，若 `routerd-ndpi-agent.service` 已啟用或正在執行，則會重新啟動。確認原生後端是否生效：

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

回應中應包含 `"libndpiLoaded": true`。

## 設定

啟用 `FirewallEventLog` 資源。routerd 隨後啟動
`routerd-firewall-logger` 與 `routerd-ndpi-agent`，記錄程式透過 Unix
socket 將取樣封包直接送給代理程式。不再使用獨立的分類程序，也沒有內建分類器的替代路徑。
