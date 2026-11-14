# DHCPv4ServerLeaseSync

[各資源的設定](../resources.md#dhcpv4serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

以 SSH 上的 rsync 將 DHCPv4 伺服器租約檔與現有固定保留狀態複製到對等主機，並非 DHCP 容錯移轉協定。

## 指定條件與參照關係

- source.resource 與至少一個 target.host 必填。不同於狀態值參照，一般驗證僅檢查寫法與類型，不驗證 DHCPv4Server 是否存在；不存在的名稱會退回平台預設租約路徑。
- 目標路徑與本機來源的絕對路徑相同，YAML 無目標路徑欄位。具名來源的 stickyHoldDays 為正時，若存在則複製 dhcp-sticky.db 及 -wal，不複製 -shm。主要租約檔必須存在。
- 每次執行使用 rsync -a --checksum --delay-updates，預設 rsync 無活動限制 60 秒、命令執行限制 2 分鐘。SSH 預設 BatchMode=yes、ConnectTimeout=10，可由 sshOptions 覆寫。Synced 僅表示檔案複製成功，不證明對端重新載入租約或客戶端連續性。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    source: # 選填; object
      resource: "<string>" # 必填; string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        options: # 選填; array
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
| `spec.source` | 必填含 resource 的來源描述，此處無檔案路徑覆寫。 |
| `spec.source.resource` | 必填 DHCPv4Server 裸名或 DHCPv4Server/name。使用其非空 leaseFile，否則為 <StateDir>/dnsmasq/dnsmasq.leases。並非狀態來源，無 field/optional。 |
| `spec.command` | 選用執行檔名稱或路徑，預設 rsync；直接接收 rsync 引數，不是殼層命令字串。 |
| `spec.interval` | Go duration，預設 30s。距上次成功 syncedAt 未達期間則略過；控制器另以 30 秒週期及訂閱事件執行。零或負值僅停用略過，不會加快週期排程。 |
| `spec.targets` | 必填非空對等目標清單，逐一傳送檔案，第一個失敗記錄 Error/SyncFailed，不呼叫遠端 routerd API。 |
| `spec.targets[].name` | 選用狀態標籤，預設空，不用於選擇目的主機或來源。 |
| `spec.targets[].host` | 必填 SSH 主機或位址，不可含 CR/LF，須事先具備可達路由及非互動 SSH 認證。 |
| `spec.targets[].user` | 選用 SSH 使用者，空值省略 user@ 並由 SSH 設定決定，不可含 CR/LF。 |
| `spec.targets[].sshOptions` | 選用 SSH 引數清單，組合至 rsync -e。未指定對應鍵時補上 -o BatchMode=yes 與 -o ConnectTimeout=10，項目不可含 CR/LF。 |
| `spec.targets[].options` | 選用額外 rsync 引數，附於預設之後。提供 --timeout 或 --timeout=... 時不補入 --timeout=60，但不變更獨立的 2 分鐘程序限制；不可含 CR/LF。 |
| `spec.when` | 選用共用狀態條件，預設無條件。應以實際主用角色狀態避免備用端寫入，本資源不自行協商租約檔擁有者。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
