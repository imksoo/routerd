---
title: nDPI agent native package
---

# nDPI 代理程式原生套件

![Diagram showing the nDPI native agent package overlaying the static routerd release archive with a libndpi-linked routerd-ndpi-agent, installer self-test, and runtime agent socket status](/img/diagrams/operations-ndpi-agent-libndpi.png)

routerd 的一般 Linux 發布封存檔以 `CGO_ENABLED=0` 建置，確保封存檔內所有 routerd 二進位檔均為靜態連結。選用的 `routerd-ndpi-agent-libndpi` 封存檔是針對需要原生 nDPI 分類功能的主機所提供的例外套件。

此封存檔僅包含下列項目：

- `bin/routerd-ndpi-agent`
- `share/doc/README.md`
- `share/doc/VERSION`
- `share/doc/TARGET`

此二進位檔以 `CGO_ENABLED=1 -tags libndpi` 建置，並連結至目標系統的 `libndpi` 執行期函式庫。它的用途是覆蓋已安裝一般 routerd 封存檔之主機上的對應二進位檔。

## Install

同時下載一般 routerd 發布封存檔與對應的原生代理程式封存檔，並讓安裝程式以單一交易方式套用。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

主機需要安裝與封存檔建置時具有相同共用函式庫 ABI 的 `libndpi` 執行期套件。在 Debian/Ubuntu 上，可透過以下指令安裝選用的執行期相依套件：

```sh
sudo apt-get install libndpi-bin
```

確認原生後端是否已啟用：

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

回應中應包含 `"libndpiLoaded": true`。

## Upgrade note

一般 routerd 封存檔內含預設的靜態版 `routerd-ndpi-agent`。
升級時，若現有原生代理程式的 `selftest` 回傳 `"libndpiLoaded": true`，而封存檔中的代理程式不回傳，則 `install.sh` 會保留現有的原生代理程式。

若主機需要原生應用層分類功能，請在執行一般安裝程式時加上 `--with-ndpi`。若最終安裝的代理程式未回傳 `"libndpiLoaded": true`，安裝程式將會失敗。此機制可防止靜態版退路在無聲無息的情況下取代原生 nDPI 的預期行為。

若為新安裝，或希望明確以原生代理程式封存檔為正本，請傳入 `--with-ndpi-archive PATH`。安裝程式會驗證封存檔的目標標記、拒絕不安全的 tar 路徑、驗證相鄰的 `.sha256` 檔案（如存在），並確認封存檔中的代理程式是否回傳 `"libndpiLoaded": true`。若覆蓋作業失敗，將回滾整個安裝。

## Configure

`routerd-dpi-classifier` 需以 `--engine auto` 或 `--engine ndpi-agent` 搭配指向代理程式 socket 的 `--ndpi-agent-socket` 進行設定。
建議使用 `auto`，以便在原生代理程式無法使用時退回至內建分類器。

已棄用的 `--ndpi-reader` 選項無法啟用原生 nDPI 分類。
