# NAT44SessionSync

[各資源的設定](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

使用 SSH 事件串流或持續執行的 conntrackd FTFW 對等連結複製 Linux conntrack 狀態，不是 FreeBSD PF 狀態同步。

## 指定條件與參照關係

- mode 預設 event-stream。兩種模式的 YAML 驗證都需要非空 snatAddresses 或 natRules。event-stream 另需 targets；conntrackd 需 conntrackd.interface/localAddress/peerAddress 且 when 必須空。
- event-stream 先協調快照，再消費 conntrack -E extended 事件，通常最多 5 秒或 512 操作一批，經 SSH 套用新增/刪除指令。支援 IPv4 TCP/UDP/ICMP；缺少狀態的 TCP 新增略過。不複製完整 TCP 視窗/序號追蹤，並非完整既有 TCP 容錯切換機制。
- conntrackd 模式兩端持續複製，停止舊 SSH 工作。snatAddresses/natRules/excludeNatRules 雖驗證，但不用於 conntrackd 篩選；產生器允許 TCP/UDP/ICMP，排除回環與 ignoreIPv4。targets/conntrackCommand/interval 不控制 conntrackd。
- Linux conntrackd 驗證另需明確、非 optional 的 Sysctl net.netfilter.nf_conntrack_tcp_be_liberal，value=1、有效 expectedValue=1 且 runtime 啟用。只有 SysctlProfile 不足。通訊/ignoreIPv4 使用純 IPv4；不自動探索對等節點或分發密鑰。
- 請只使用一個 conntrackd 資源，產生的實例共用 /run/routerd/conntrackd.ctl 與鎖路徑。目前 routerd-vrrp-vmac 升主整合明確使用 /etc/conntrackd/routerd-dslite-sessions.conf，因此預期 metadata.name=dslite-sessions；任意名稱不會自動整合其快取。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 選填; string
    conntrackd: # 選填; object
      ignoreIPv4: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      localAddress: "<string>" # 必填; string
      peerAddress: "<string>" # 必填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    excludeNatRules: # 選填; array
      - "<string>" # string
    interval: "<string>" # 選填; string
    mode: "event-stream" # 選填; string; enum=["event-stream", "conntrackd"]
    natRules: # 選填; array
      - "<string>" # string
    snatAddresses: # 選填; array
      - "<string>" # string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        restoreCommand: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.mode` | event-stream（預設）或 conntrackd。前者透過 SSH 轉送選定組合；後者在 Linux systemd 執行原生 FTFW 複製。 |
| `spec.interval` | 不支援輪詢間隔。event-stream 拒絕非空值，conntrackd 接受但不使用。 |
| `spec.conntrackCommand` | event-stream 執行檔名稱/路徑，預設 conntrack。視為單一執行檔，不是 shell 命令列；conntrackd 忽略。 |
| `spec.snatAddresses` | 選擇 event-stream 組合的純 IPv4 清單，預設空。與 natRules 解析位址合併、去重排序。此清單或 natRules 至少一項必填；conntrackd 不作複製篩選。 |
| `spec.natRules` | 選用 NAT44Rule/name 或單純規則名，只驗證格式不驗證存在。event-stream 先讀宣告 snatAddress，再讀 status.snatAddress；任一未排除規則未解析會使工作 Pending。不解析 translation.address/pool。 |
| `spec.excludeNatRules` | 選用 NAT44Rule/name 或單純名稱，在解析 natRules 時略過。預設空，不扣除明確 snatAddresses，也不篩選 conntrackd。 |
| `spec.targets` | event-stream 必填非空清單，每個目標經 SSH 接收還原操作。conntrackd 不使用。 |
| `spec.targets[].name` | 選用描述/狀態名稱，預設空；實際目的由 host 決定。 |
| `spec.targets[].host` | 必填非空 SSH 主機或 SSH 別名。遠端使用者須有執行 conntrack 還原的權限，不自動佈建憑證。 |
| `spec.targets[].user` | 選用遠端 SSH 使用者，預設空交由 SSH 設定；非空產生 user@host。 |
| `spec.targets[].sshOptions` | 選用 SSH 引數清單，未明確覆寫時加入 BatchMode=yes 與 ConnectTimeout=10。每項為單一引數，不可含換行。命令操作另有實作的 2m 逾時。 |
| `spec.targets[].restoreCommand` | 選用遠端 conntrack 命令 argv，預設 [conntrack]。可用獨立引數提供權限包裝，如 [sudo, -n, conntrack]。不是定期完整狀態傳輸命令。 |
| `spec.conntrackd` | conntrackd 模式必填物件，event-stream 不使用。兩個 HA 節點都須持續執行並允許複製連結。 |
| `spec.conntrackd.interface` | 必填複製介面。有設定邏輯別名時解析，否則視為核心名稱。此處無一般介面資源存在檢查；產生器拒絕空白。 |
| `spec.conntrackd.localAddress` | 必填純 IPv4 本機複製位址，須已設定於主機；不會指派位址。 |
| `spec.conntrackd.peerAddress` | 必填純 IPv4 遠端複製目的，不設定探索或多播成員。 |
| `spec.conntrackd.port` | UDP 複製連接埠，省略/0 在產生時為 3780，正值 1–65535。Go 執行接受 0 作預設，但 JSON Schema 最小值為 1。 |
| `spec.conntrackd.ignoreIPv4` | 選用純 IPv4 排除清單，YAML 預設空。產生器固定加入 127.0.0.1 並排除 IPv6 ::1，IPv4 去重排序，產生時拒絕無效值。 |
| `spec.when` | event-stream 的選用共用啟用條件，預設無條件，可控制 SSH 工作。conntrackd 必須完全空白，讓待命端也持續複製。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/nat44sessionsync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/nat44sessionsync.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/render/conntrackd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/conntrackd.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `mode`, `streamState`, `sessionCount`, `remoteSessionCount`, `targetCount`, `snatAddresses`, `targets`, `deleteOK`, `deleteMissing`, `deleteFailed`, `pruneOK`, `pruneMissing`, `pruneFailed`, `pruneCandidateCount`, `insertOK`, `insertExisting`, `insertFailed`, `queuedEventCount`, `lastBatchEvents`, `resyncCount`, `syncedAt`, `lastEventAt`, `lastBatchAt`, `lastResyncAt`, `dryRun`
