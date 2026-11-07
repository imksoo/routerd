---
title: 開發時確認
---

# 開發時確認

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd 將兩條自動化流程分開管理。

- CI workflow 負責確認一般的 push 與 pull request。
- release workflow 在推送 release tag 後產生發布封存檔。

release workflow 涵蓋多種 OS 與 CPU 架構，並公開 GitHub Release 的成果物。
因此與一般 CI 分開維護。


## 測試與 harness 設計的強制規範

適用於單元測試、離線 fixture、runtime smoke、release qualification，以及可重用的舊 helper、範本和複製來源，包括目前沒有 caller 的程式碼。修改時必須說明產品要求、必要直接證據和觀測限制。

- 保留觀測與診斷日誌，與產品合否分離。沒有具體產品要求，不得將 journal、進展計數器、latency 統計或整機診斷變成合否條件。
- PASS／產品 FAIL 必須依據已完成且可歸屬到目標的直接證據。API／SSH 波動、採集失敗、缺失或損壞輸入、guest command 未完成和 timeout 應維持為觀測或基礎設施結果，不得轉成零、無違規、成功拒絕或實測產品違規。
- 減少無依據的速度斷言、重複 probe 和 gate。保留契約期限和停止掛起工作的有界 watchdog，並與產品效能測量區分。
- 刪除暫存資料前保留失敗的原始嘗試、來源身分、exit、時間和 cleanup 結果。僅清理本次擁有的資源，不得隱藏失敗或刪除產品證據。
- 不得未經原因調查重複失敗的實機試驗。達到既有 retry、時間或費用預算時停止，報告失敗、不確定性、剩餘預算和修復方案後再繼續。僅對有依據的失敗或缺失觀測進行有界重採集，並保留每次嘗試。
- 審計實際入口、選擇的來源路徑和 SHA、相依關係及最終結果傳播。候選或 mock 驗證只證明其範圍，不能證明運行入口已選擇候選或真實轉送成功。
- 區分實作、準備候選、運行程式碼、保存原本重播、刻意修改的 fixture 和未驗證行為。保留原本負例，不得把歷史失敗改寫為 PASS。
- 重用舊程式碼或尚未連接的程式碼時也必須遵守。封存原本保持不變，修復可重用來源；不得以目前沒有 caller 為由忽略缺陷。

這些規範不放寬真實功能要求、安全與所有權檢查或費用限制。缺少必要證據不能建立 PASS。保留正常通訊和直接違規的證明，只刪除與診斷之間無依據的耦合。

正確例：curl exit 0／HTTP 200 且具備必要 body 與路徑證據時，可將可選 latency 缺失記為診斷並 PASS。錯誤例：WireGuard 讀取失敗證明禁止 AllowedIP 不存在；invalid apply 的 timeout／強制終止計為成功拒絕；同一網路堆疊 ping 成功證明通道轉送。

## CI workflow

`.github/workflows/ci.yaml` 在分支 push 與 pull request 時執行。
使用 Ubuntu runner，確認在進入程式碼審查前應保持綠燈的範圍。

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

當變更涉及 `webconsole/`、已簽入的靜態資源、共用 quality workflow 或 Makefile 時，CI 還會
執行 Web Console 專用關卡：`npm ci`、全部相依套件及正式環境相依套件的 high severity
audit、TypeScript typecheck、正式環境 build，以及產生資源的差異檢查。

CI workflow 不公開發布成果物。
發布封存檔由日期格式的 tag 觸發 `Release` workflow 產生。

## pre-commit hook

儲存庫中附有可選用的 pre-commit hook。

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

啟用後，`git commit` 執行前會進行以下確認。

```sh
go test ./...
make check-schema
```

任一項失敗，commit 即中止。
可在 CI 之前提早發現 schema 差異或測試失敗。

若緊急情況下需要在本地 commit，可指定以下環境變數。

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

請僅在後續修正明確的情況下使用。
push 分支後，CI 仍會執行。

## Linux DNS resolver runtime 檢查

在具備 iproute2、dnsmasq、`dig`、curl、Go 和 network namespace 的隔離 Linux 環境中執行：

```sh
sudo ./tests/netns/dns-resolver-runtime.sh
```

測試使用準備好的 1.5 runtime 設定啟動實際的 `routerd-dns-resolver`，再由獨立 `dig` 用戶端查詢。
本機 zone 和條件 upstream 轉送必須透過 UDP 與 TCP 傳回預期結果。傳送 SIGHUP 重新載入變更後的設定後，
必須傳回新的 zone 答案；停止並重新啟動 worker 後，本機答案和 TCP 轉送也必須恢復。成功時刪除暫存狀態和記錄，
失敗時保留暫存目錄供診斷。此項只檢查 resolver 程序，不涵蓋 routerd controller 或 service manager；
也不涵蓋 DHCP lease 更新或 tombstone、negative reply、DoT/DoH、controller 驅動的設定更新或 FreeBSD runtime。

## Linux PortForward DNAT 與 hairpin 檢查

在具備 iproute2、nftables、curl、Python 3、Go 和 network namespace 的隔離 Linux 環境中執行
`sudo ./tests/netns/nat44-portforward.sh`。測試先驗證準備好的 routerd 1.5 YAML，再於私有 router namespace 中執行實際的 NAT44 controller。
獨立 WAN、LAN server 和 LAN client namespace 檢查 WAN DNAT、LAN hairpin 來源位址轉換，以及直接 LAN 通訊的來源位址保留。
撤銷規則後，既有 WAN TCP 連線必須 reset，routerd NAT table 必須移除，新的 WAN 連線不得抵達 backend。成功時刪除暫存狀態和記錄，
失敗時保留暫存目錄供診斷。此測試不匯入 1.0 設定或狀態，也不涵蓋 package install、service manager、pinhole、session 同步、
多 backend ingress 或 FreeBSD。

## Linux NAT44 source SNAT 選擇器檢查

在具備 iproute2、nftables、curl、Python 3、Go 和 network namespace 的隔離 Linux 環境中執行
`sudo ./tests/netns/nat44-source-snat.sh`。測試先驗證準備好的 routerd 1.5 設定，再於私有 router namespace 中執行實際 NAT44 controller 與 nftables。
獨立 WAN HTTP peer 觀察轉送流量的來源位址。只有符合來源與目標 CIDR 的流量會使用設定的 SNAT 位址；排除的目標及來源範圍外的用戶端必須保留原始位址。
變更 `snatAddressFrom` 的參照後，新連線必須使用新的來源位址；撤銷規則後應移除 NAT table 並恢復原始來源位址。成功時刪除暫存輸出，失敗時只保留本次診斷目錄。

## Linux LocalServiceRedirect DNS 攔截檢查

在具備 iproute2、nftables、dnsmasq、BIND `dig`、Python 3、Go 和 IPv6 核心支援的隔離 Linux 環境中執行
`sudo ./tests/netns/local-service-redirect.sh`。測試驗證準備好的 routerd 1.5 設定，然後在私有 router namespace 中執行實際 NAT controller 與 nftables。
獨立 LAN 用戶端和 WAN 側 DNS 伺服器驗證：只有符合目標位址集、連接埠和入口介面的 IPv4/IPv6 TCP/UDP 查詢會抵達本機 DNS；不符合的目標、連接埠或介面仍會抵達 upstream。
變更所選 LAN 介面後，重新導向必須隨之切換；撤銷後應恢復 upstream 回應並刪除兩個 NAT table。測試從新的 routerd 1.5 設定和狀態開始，不匯入 1.0 設定或狀態。
成功時刪除暫存輸出；失敗時只保留本次用於重現的診斷目錄。
