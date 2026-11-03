---
title: nDPI agent 原生套件
---

# nDPI agent 原生套件

routerd 1.5 提案版使用選用 Debian 套件 `routerd-ndpi-agent-libndpi`。
Linux 本體維持靜態連結；此 agent 使用 `CGO_ENABLED=1 -tags libndpi` 與 OS 的共享程式庫。
目前發布建構目標為 Ubuntu 24.04 amd64，尚未公布公共 1.5 套件或 apt 儲存庫。

取得並驗證同版本的本體與附加套件，使用實際檔名安裝或一起更新：

```sh
sudo apt install ./routerd_<version>_amd64.deb \
  ./routerd-ndpi-agent-libndpi_<version>_amd64.deb
```

附加套件依賴完全相符的本體版本。`dpkg-shlibdeps` 從實際 ELF 計算程式庫依賴，由 apt
解決。不要用封存檔或手動複製覆寫套件檔案。

原生 agent 仍位於 `/usr/sbin/routerd-ndpi-agent`。附加套件以自己擁有的 `dpkg-divert`
將本體的靜態 agent 分流至 `/usr/sbin/routerd-ndpi-agent.routerd-static`，因此本體更新
不會覆寫原生版本。拒絕接管其他套件或管理員的 diversion。

更新會先停止自有 main 與 helper，再置換並恢復服務；否則 main 可能在置換期間
自動重啟 helper。附加套件的變更也可能中斷通訊與觸發協定重新收斂。
首次安裝而未啟用設定時不啟動服務。

```sh
/usr/sbin/routerd-ndpi-agent selftest
sudo curl --fail --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

確認 `libndpiLoaded: true`。實際程式庫的 TLS 自測應給出預期 SNI 與
`reason: libndpi_protocol_detection`。僅安裝檔案不代表已驗證實際轉送流量分類。

```sh
sudo apt remove routerd-ndpi-agent-libndpi
```

移除附加套件後，dpkg 恢復目前本體的靜態 agent 並恢復先前運作的自有服務，保留設定與 DB。
靜態 agent 不載入 libnDPI，因此仍需要原生分類時不要移除此套件。

建構使用 `make dist-ndpi-agent-libndpi-deb ROUTERD_OS=linux GOARCH=amd64`，需要 Go、
C compiler、`libndpi-dev`、`pkg-config`、`dpkg-dev`、`file` 與 `readelf`。
隔離 OS 的安裝、更新、移除與合格標準見[英文測試流程](https://routerd.net/docs/operations/ndpi-agent-libndpi#build-and-test)。
