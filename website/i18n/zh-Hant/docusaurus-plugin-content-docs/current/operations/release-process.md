---
title: Release process
---

# 發布流程

![Diagram showing release process from clean working tree and changelog through date-based version, schema regeneration, tag creation, GitHub Actions archives, checksums, and latest download URLs](/img/diagrams/operations-release-process.png)

routerd 採用以日期為基礎的版本號。
執行檔版本號、發布標籤與發布封存檔名稱均使用 `vYYYYMMDD.HHmm` 格式。
日期與時間預設以 `Asia/Tokyo` 為基準計算。

## 自動發布

確保工作目錄乾淨後，執行發布輔助指令：

```sh
make release
```

輔助程式會使用 `Asia/Tokyo` 的當前日期與起始時間，更新執行檔的版本號字串，將目前 `Unreleased` 的 changelog 項目升格至新的發布標籤，並保留一個新的空 `Unreleased` 標題。接著重新產生（render）儲存庫管理的 schema，提交變更，建立標籤，並同時將 `main` 與標籤推送至遠端。

例如，在 JST 15:30 開始的發布會使用 `.1530` 後綴。

常用選項如下：

```sh
scripts/release.sh --dry-run
scripts/release.sh --date 20260510
scripts/release.sh --timezone UTC
scripts/release.sh --no-push
```

執行輔助程式前，工作目錄必須是乾淨狀態。
功能變更與 changelog 變更請先提交。輔助程式只建立發布提交。
changelog 中請以 `## Unreleased` 作為最前面的發布區塊，且該區塊必須有項目才能建立發布。

推送發布標籤後，GitHub Actions 工作流程將會啟動。
`Release` 工作流程會建置下列目標：

- `linux-amd64`
- `linux-arm64`
- `freebsd-amd64`
- `freebsd-arm64`
- `routerd-ndpi-agent-libndpi-linux-amd64`（選用的原生 nDPI 代理程式覆蓋封存檔）

每個目標的封存檔以兩個名稱發布：

- 指向特定發布的 `routerd-<tag>-<os>-<arch>.tar.gz`
- 以固定 URL 下載最新版的 `routerd-<os>-<arch>.tar.gz`
- 選用原生 nDPI 代理程式覆蓋用的
  `routerd-ndpi-agent-libndpi-<tag>-linux-amd64.tar.gz` 與
  `routerd-ndpi-agent-libndpi-linux-amd64.tar.gz`

Linux 封存檔以 `CGO_ENABLED=0` 建置，因此封存檔內的 routerd 二進位檔為靜態連結，不依賴目標主機的 glibc 版本。工作流程在封裝 Linux 封存檔前會執行 `make check-linux-static`。選用的原生 nDPI 代理程式封存檔刻意分離，因為它以 `CGO_ENABLED=1 -tags libndpi` 建置並連結至主機的 `libndpi` 執行期，不包含在一般靜態 Linux 封存檔中。

每個名稱均附有對應的 `.sha256` 檔案。
封存檔包含下列內容：

- `bin/`：`routerd`、`routerctl` 及受管理常駐程式的二進位檔
- `install.sh`：POSIX sh 安裝程式
- `uninstall.sh`：POSIX sh 解除安裝程式
- `etc/routerd/router.yaml.sample`：不含密鑰的設定範例
- `systemd/` 或 `rc.d/`：目標 OS 的服務範本
- `share/doc/`：README、VERSION、LICENSE、第三方授權清單

原生 nDPI 代理程式覆蓋用封存檔僅包含 `bin/routerd-ndpi-agent` 與最精簡的文件。希望以 `libndpiLoaded=true` 執行 `routerd-ndpi-agent` 的主機，應與一般 routerd 封存檔一同安裝：

```sh
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

安裝程式的下載動作保持明確。它不會自行取得功能封存檔。發布 runbook 中請在呼叫 `install.sh` 前先下載封存檔與對應的 `.sha256` 檔案。

工作流程會將版本化封存檔、固定名稱封存檔及各自的 `.sha256` 檔案上傳至 GitHub Release 頁面。
快速入門文件使用固定 URL 下載最新版：

```text
https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
```

只有在需要固定特定發布的 runbook 中才使用版本化 URL。

一般分支的推送與 pull request 使用另一個 `CI` 工作流程，該工作流程只執行開發期間的檢查，不發布發布成品。pre-commit hook 與 CI 的範圍請參閱[開發時的檢查](/docs/operations/development)。

## 職責分工

安裝邏輯位於 `install.sh`。
Makefile 僅供開發作業使用，包含建置、測試、schema 檢查、範例驗證、網站建置及發布封存檔產生等。
發布封存檔不含 Makefile。
這樣可將面向終端使用者的安裝與升級行為集中在單一 script 中。

開發時的測試使用 Makefile 目標：

```sh
make test
make check-schema
make validate-example
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION="$(git describe --tags --abbrev=0)"
make dist-ndpi-agent-libndpi ROUTERD_OS=linux GOARCH=amd64 VERSION="$(git describe --tags --abbrev=0)"
```

部署時的冒煙測試使用 `install.sh`。
安裝完成後，若 routerd 的唯讀 status socket 存在，`install.sh` 會呼叫 `routerctl get status`。
GitHub 發布工作流程也會展開每個封存檔，並在系統外的臨時 prefix 下執行 `install.sh`。
此冒煙測試確認封存檔可在不使用 Makefile 的情況下正常安裝與解除安裝。
由於安裝相依套件是目標路由器主機的職責，CI 冒煙測試會傳入 `--no-install-deps`。

在路由器主機上安裝發布封存檔：

```sh
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

`install.sh` 會將二進位檔複製至 `/usr/local/sbin`、安裝服務範本，並寫出 `router.yaml.sample`。
執行開始時會偵測 OS 的套件管理器，並在未傳入 `--no-install-deps` 的情況下安裝已知的執行期套件。
不會覆蓋現有的 `/usr/local/etc/routerd/router.yaml`。
若發現現有的 `/usr/local/sbin/routerd`，安裝程式會自動切換至升級模式。
此時會顯示舊版與新版的 `routerd --version` 輸出，替換二進位檔與服務範本，保留設定與狀態，並在服務已在執行時重新啟動 `routerd.service` 或 FreeBSD 的 `routerd` rc.d 服務。
在 systemd 主機上，重新啟動後會等待 `routerd.service` 的 status socket，並僅重新啟動仍以已刪除的升級前二進位檔執行、或在輔助程序啟動後 unit 檔案已更新的作用中 routerd 輔助服務。
替換的檔案在替換前會複製為 `*.backup.YYYYMMDDHHMMSS`。
若只想替換檔案而不重新啟動服務，請傳入 `--no-restart`。
若要顯示預計的檔案與服務管理器變更而不實際執行，請傳入 `--dry-run`。
若要輸出 shell 追蹤，請傳入 `--verbose`。
若要保留 `router.yaml.sample` 不變，請傳入 `--no-config-update`。
若要略過 OS 套件安裝，請傳入 `--no-install-deps`。
若要在不變更主機的情況下列出套件與指令清單，請傳入 `--list-deps`。
若要在安裝套件後、複製 routerd 檔案前即結束，請傳入 `--deps-only`。
若要加入選用的 Tailscale 套件與指令檢查，請傳入 `--with-tailscale`。
若要在新安裝時呼叫主機的服務管理器，請傳入 `--enable-service` 或 `--start-service`。
安裝完成後，若 routerd 的唯讀 status socket 存在，script 會執行 `routerctl get status`。

安裝程式不會對下列執行期與狀態路徑做任何變更：

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

## 授權清單

routerd 本體以 BSD 3-Clause License 發布。
發布封存檔與 Live ISO 包含採用其他授權的第三方軟體。
發布前請重新產生清單：

```sh
make third-party-licenses
```

產生的 `THIRD_PARTY_LICENSES.md` 記錄 Go 模組的授權檔案與 Live ISO 套件的授權中繼資料。Live ISO 是集合性發布物。
採用 GPL 授權的套件各自保留其授權與原始碼取得方式，不視為以 GPL 著作重新授權整個 ISO。

## 執行期相依套件

`install.sh` 將相依套件的安裝納入與二進位檔安裝相同的面向終端使用者的流程，以避免使用 Makefile 作為另一條安裝路徑。

Debian 與 Ubuntu 上，安裝程式使用 `apt-get` 安裝下列套件：

```text
ca-certificates curl dnsmasq-base nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables keepalived
```

Fedora 系統上，安裝程式使用 `dnf` 安裝下列套件：

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod radvd strongswan iptables keepalived
```

Arch 系統上，安裝程式使用 `pacman` 安裝下列套件：

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod radvd strongswan iptables keepalived
```

FreeBSD 上，安裝程式使用 `pkg` 安裝下列套件：

```text
ca_root_nss curl dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq chrony strongswan
```

FreeBSD 的 `pf`、`ifconfig`、`route`、`service`、`sysrc`、`cron` 均為基本系統工具，不以套件方式安裝，僅確認指令是否存在。

安裝相依套件後，script 會確認預期的指令是否存在。
由於套件名稱因發行版而異，若找不到指令，視為警告而非致命錯誤。
確認相依套件清單請使用：

```sh
./install.sh --list-deps
```

## 解除安裝

若要在不影響狀態的情況下移除已安裝的檔案，請使用 `uninstall.sh`：

```sh
sudo ./uninstall.sh --yes
```

預設的解除安裝會停止並停用服務，移除 routerd 二進位檔、服務範本與執行期檔案。
`/usr/local/etc/routerd`、`/var/lib/routerd`、`/var/db/routerd`、`/var/log/otelcol` 會保留。

完全移除的選項需明確指定：

```sh
sudo ./uninstall.sh --yes --purge-config
sudo ./uninstall.sh --yes --purge-state
sudo ./uninstall.sh --yes --all
```

若要在不變更主機的情況下預覽移除內容，請使用 `--dry-run`。

## 手動觸發

若標籤已存在，也可透過 GitHub Actions 的 `workflow_dispatch` 輸入啟動工作流程：

```text
tag = vYYYYMMDD.HHmm
```

工作流程會在建置前 checkout 該標籤。

## 備援方案

若 GitHub Actions 無法使用，可在本機建置相同的封存檔：

```sh
tag=$(git describe --tags --abbrev=0)
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION="$tag"
make dist ROUTERD_OS=freebsd GOARCH=amd64 VERSION="$tag"
make dist-ndpi-agent-libndpi ROUTERD_OS=linux GOARCH=amd64 VERSION="$tag"
```

再透過 GitHub CLI 建立發布：

```sh
tag=$(git describe --tags --abbrev=0)
gh release create "$tag" \
  "dist/linux-amd64/routerd-${tag}-linux-amd64.tar.gz" \
  "dist/linux-amd64/routerd-${tag}-linux-amd64.tar.gz.sha256" \
  dist/linux-amd64/routerd-linux-amd64.tar.gz \
  dist/linux-amd64/routerd-linux-amd64.tar.gz.sha256 \
  "dist/freebsd-amd64/routerd-${tag}-freebsd-amd64.tar.gz" \
  "dist/freebsd-amd64/routerd-${tag}-freebsd-amd64.tar.gz.sha256" \
  dist/freebsd-amd64/routerd-freebsd-amd64.tar.gz \
  dist/freebsd-amd64/routerd-freebsd-amd64.tar.gz.sha256 \
  "dist/linux-amd64/routerd-ndpi-agent-libndpi-${tag}-linux-amd64.tar.gz" \
  "dist/linux-amd64/routerd-ndpi-agent-libndpi-${tag}-linux-amd64.tar.gz.sha256" \
  dist/linux-amd64/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz \
  dist/linux-amd64/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256 \
  --title "routerd ${tag}" \
  --generate-notes \
  --verify-tag
```
