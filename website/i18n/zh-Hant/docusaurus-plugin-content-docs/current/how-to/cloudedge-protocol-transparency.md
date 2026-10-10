---
title: CloudEdge 協定透明性驗收檢驗
---

# CloudEdge 協定透明性驗收檢驗

![CloudEdge 協定透明性探針的 FTP、NFS、大容量傳輸、PMTU、來源 IP 保留、no-NAT 證據的流程](/img/diagrams/how-to-cloudedge-protocol-transparency.png)

這是一個不使用雲端的線束計畫，用於驗證 CloudEdge mobility 對連線導向的協定（這些協定對 NAT、輔助 ALG、動態連接埠、MTU/PMTU 行為敏感）的透明性。實際的即時運行將由實驗室操作員稍後執行。本文件和 `scripts/` 目錄下的指令碼僅準備契約和證據格式。

## 目標

對邏輯共享子網路（展示中為 `10.77.60.0/24`）上的流量，證明以下幾點：

- 無 NAT：伺服器將用戶端站點的 mobility `/32` 辨識為對等位址。
- 用戶端的預設閘道未從本機站點變更。
- FTP 主動模式和被動模式均在無 NAT ALG 的情況下完成資料傳輸。
- 透過 `rpcbind` 的 RPC 端點探索和 NFSv3 的掛載/讀寫在站點間正常運作。
- 大容量傳輸在無 PMTU 黑洞的情況下完成。
- MSS/PMTU 證據記錄了 overlay MTU、路由 MTU/advmss（如可用）、已設定的 MSS clamp 值。

## 最小即時矩陣

在常規 D3 有向矩陣已全部通過後，運行協定探針。使用 2 個代表性對：

| 對 | 理由 |
| --- | --- |
| `aws -> azure` | 兩端均使用雲端提供者捕獲的雲間 overlay 路徑 |
| `aws -> onprem` | on-prem 端使用 proxy-ARP/VRRP 權限的雲端-on-prem 路徑 |

在場景目錄中，這編碼為 `examples/cloudedge-acceptance-scenarios.json` 的 `d11-protocol-transparency`。

如需擴大對等性驗證，可新增 `azure -> oci`、`oci -> aws`、反向等，但最小驗收檢驗應控制在單次 4 站點實驗室視窗內可執行的規模。

## 線束

封裝器如下：

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
  scripts/cloudedge-protocol-probe.sh \
    --pairs aws:azure,aws:onprem \
    --bytes 104857600 \
    --out evidence/protocol-probe.json
```

完整驗收場景使用相同封裝器：

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
MATRIX_RUNNER=scripts/runners/cloudedge-matrix-runner.sh \
scripts/cloudedge-acceptance.sh run \
  --scenario d11-protocol-transparency \
  --out evidence/d11-protocol \
  --commit <routerd-commit>
```

輸出透過 `scripts/cloudedge-protocol-result-schema.json` 驗證，並匯入到 `result.json` 的 `protocols` 物件下。

## 運行器契約

`scripts/runners/cloudedge-protocol-runner.sh` 實作 `PROTOCOL_PROBE_RUNNER`。有意透過環境變數參數化，不包含提供者帳戶 ID、資源 ID、secret。

每個站點所需的設定：

```sh
export CE_AWS_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AWS_CLIENT_IP=10.77.60.11
export CE_AZURE_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AZURE_CLIENT_IP=10.77.60.12
export CE_ONPREM_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export ONPREM_CLIENT_IP=10.77.60.10
export SSH_KEY_FILE=<private-key>
export SSH_USER=ubuntu
export CLIENT_SSH_USER=ubuntu
```

協定相關變數：

```sh
export CE_PROTOCOL_INSTALL=1
export CE_PROTOCOL_CONFIGURE_SERVICES=1
export CE_PROTOCOL_FTP_PASSIVE_PORTS=40000:40100
export CE_PROTOCOL_BULK_BYTES=104857600
export CE_PROTOCOL_PMTU_SIZE=1300
export CE_PROTOCOL_OVERLAY_IFACE=wg-hybrid
export CE_PROTOCOL_MSS_CLAMP=1340
```

每個操作均可在不編輯運行器的情況下覆寫：

```sh
export CE_PROTOCOL_FTP_ACTIVE_COMMAND='...'
export CE_PROTOCOL_NFS_COMMAND='...'
```

封裝器對每個對呼叫以下操作：

| 操作 | 斷言 |
| --- | --- |
| `setup` | 啟用時安裝/設定 `vsftpd`、`rpcbind`、NFS 伺服器/用戶端工具、`iperf3` |
| `ftp-active` | FTP `PORT` 模式資料通道完成 |
| `ftp-passive` | FTP 被動模式資料通道完成 |
| `nfs` | NFSv3 掛載 + 要求位元組數的寫入/讀取完成 |
| `rpc` | `rpcinfo -p` 探索到 `rpcbind` 和至少 1 個動態 RPC/NFS 連接埠 |
| `bulk` | `iperf3 -n <bytes>` 完成，記錄吞吐量/重傳 |
| `pmtu` | DF ping 成功，記錄 overlay MTU、路由 MTU/advmss、MSS clamp |
| `source-preserved` | 伺服器端 SSH 將用戶端的 mobility `/32` 辨識為對等 IP |
| `no-nat` | 相同的對等 IP 檢查，作為顯式 no-NAT 斷言記錄 |

## Forcefrag / MSS 比較

常規運行中，routerd 導出的 MSS clamp 應能正常通過。如果需要在實驗室中驗證 P2-b 的強制分片行為，請對同一 D11 對集運行 2 次：

1. `forceFragmentIPv4: false`（預設）：TCP 傳輸應透過 MSS clamp 通過。超大的 DF 非 TCP 可能因底層網路 PMTU 而失敗。
2. 在相關 `OverlayPeer` 或 `TunnelInterface` 上 `forceFragmentIPv4: true`：相同的 DF 探針應通過，路由器證據中應顯示 `routerd_forcefrag`。

不要全域啟用 force fragmentation。限定在路徑範圍內，並在證據包中記錄 before/after 的組態摘要。

## 證據審閱清單

對 `protocol-probe.json` 中的每個對：

- `checks.ftpActive`、`ftpPassive`、`nfs`、`rpc`、`bulkTransfer`、`pmtu`、`sourceIpPreserved`、`noNat` 均為 `pass`。
- `details.sourceIpPreserved.peer_ip` 與用戶端站點的 mobility `/32` 一致。
- `details.rpc.dynamic_port` 存在且不是 `111`。
- 如果 `iperf3` 可用，`details.bulkTransfer.retransmits` 已記錄。
- `details.pmtu.overlay_mtu`、`route_mtu` 或 `route_advmss`、`mss_clamp` 已記錄。

外層 `result.json` 應包含以下通過斷言：

- `protocol_transparency`
- `ftp_active_passive`
- `nfs_rpc`
- `bulk_transfer_pmtu`
- `protocol_source_ip_preserved`
- `protocol_no_nat`

SAM E2E工具逐位元組核對FTP、NFS和CIFS的讀取與寫入內容。故障切換傳輸使用已知的64 MiB資料，下載後同時核對大小和SHA-256。注入故障前，必須確認本次傳輸工作仍在執行且已收到部分資料；無法確認進度時，以未判定狀態停止，不注入故障或重試。保留啟動、結果和失敗嘗試的證據。可選的傳輸觀測和效能值與正常故障後matrix判定分開。部署使用本次專用目錄，傳播每個必要複製或設定步驟的失敗，並在重新啟動服務前比較已安裝的binary和設定與本次資料。

L2 qualification需要同一provider和閾值設定的before與after觀測以及抑制機制記錄。before採集成功可進入下一操作，但儲存的彙總在after採集前仍為未判定；after的退出碼反映彙總結果。故障切換時長使用單調時鐘，journal關聯用UTC另行記錄。provider的VM停止實作處理`stop-active`；`drain`需要明確設定`CE_<PROVIDER>_DRAIN_INJECT_COMMAND`或`CE_DRAIN_INJECT_COMMAND`，on-premises停止也需要明確的node-stop command。僅停止keepalived不能證明node停止qualification。

## Federation qualification 證據

`scripts/cloudedge-federation-qualification.sh` 的 release qualification 要求正數 cycles 和全部 8 個場景。`--allow-skip` 允許開發用子集，其結果不會認定 release。每次嘗試使用新的證據目錄和 event ID，保留 command 的 stdout/stderr/退出碼；worker 在現有 `--duration` 預算內停止，預算包含 cleanup。失敗、觀測不足或 cleanup 未確認時，不重試，並停止後續故障和 cycle。遺留故障 marker 需要操作員在下一次嘗試前檢查。

TTL refresh 將目前 receiver event 的 `ExpiresAt` 與 sender 的延長值對照，舊 delivered 行或缺失的 `staleTTL` 不能作為證明。subscription 失敗和恢復綁定本次 event ID 與 receiver 側注入後的時間。partition 隔離記錄正常 A/B baseline、A 故障期間本次 B 配送，以及限定 group/peer 的 SLO 違規、remediation 和設定閾值對應關係；相同閾值也是有效設定。security 使用正確簽章的損壞 body 和帶錯誤簽章的有效 body，要求預期 HTTP 400/401、receiver 未接收和本次有效 event 配送。transport error 和 5xx 不證明拒絕成功。

OTel 可用性、sample 和 label 屬於診斷，缺失保持未判定，不替換成數值零或產品失敗。保存的 binary SHA-256 和產品 source commit 與 QA repository commit 分開記錄。已部署 source identity 不明或不匹配時，在 scenario 故障前停止。退出碼 1 表示觀測到的 assertion 失敗，2 表示無效輸入/setup，3 表示觀測或 provenance 不完整。這些修改通過 offline 邊界驗證，不代表 provider 或 release qualification 已完成。

開發用 partition 子集未選擇 multi-group 時，不要求 B group。實機的 Go 是可選工具。`CE_BINARY_PROVENANCE_FILE` 可指定現有 prepared/release contract，將 `routerdArtifact.commit` 和 `execution.candidate_binary_hashes` 與本次 binary SHA-256 對照。runtime commit 顯示衝突或 hash 不匹配時保留未確認狀態，不會僅憑 commit 參數確認 identity。

eventd digest 來自目標 systemd unit 的 MainPID 執行檔（`/proc/<pid>/exe`），取得後再次核對程序和執行檔。程序缺失或變化時保持未確認，PATH 中的 binary 不能證明執行中服務的 identity。成功回應中的空 OTel label 陣列仍為診斷未判定。

現有 offline assertion 按 WireGuard peer block 和完整 resource snapshot、每個 leaf 恢復的準確七個 peer、注入的 transition event identity/時間以及 transport peer/address 對應關係核對。SSE request deadline 可中斷阻塞的本文讀取。active-stable guard 要求指定檔案存在，並逐個檢查 version token。PoC bundle 將 schema 驗證記錄為 validated、unavailable 或 invalid，不新增必須安裝 validator 的 gate。capture stop 檢查現有四個 typed point record；缺失或損壞的 state 保持 PARTIAL，不執行 stop/copy。AWS fabric 核對請求的 secondary address 及其最長匹配 route，保留 raw facts 和 PARTIAL/NOT-RUN。labctl 只建置匹配的 clean 目前 checkout 並記錄 commit/tree identity；dry run 和 prebuilt 輸入不聲稱完成 build 或使用請求的 source。RR stage 拒絕必需 row 的重複或衝突，report gate 從已確認 outcome 導出。這些是 offline 檢查，不代表實機 qualification。
