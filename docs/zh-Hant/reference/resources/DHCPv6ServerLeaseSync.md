# DHCPv6ServerLeaseSync

[各資源的設定](../resources.md#dhcpv6serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

以 SSH 上的 rsync 將 DHCPv6 伺服器租約檔複製到對等主機，並非 DHCP 容錯移轉協定。

## 指定條件與參照關係

- source.resource 與至少一個 target.host 必填。不同於狀態值參照，一般驗證僅檢查寫法與類型，不驗證 DHCPv6Server 是否存在；不存在的名稱會退回平台預設租約路徑。
- 僅複製主要租約檔；不同於 DHCPv4ServerLeaseSync，此路徑不含 dhcp-sticky.db/WAL。目的端使用與本機來源相同的絕對路徑，無目標路徑覆寫或自動對端服務重新載入。
- 每次執行使用 rsync -a --checksum --delay-updates，預設 rsync 無活動限制 60 秒、命令執行限制 2 分鐘。SSH 預設 BatchMode=yes、ConnectTimeout=10，可由 sshOptions 覆寫。Synced 僅表示檔案複製成功，不證明對端重新載入租約或客戶端連續性。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    [ command: <string> ]
    [ interval: <string> ]
    source:
      resource: <string>
    targets:
      -
        host: <string>
        [ name: <string> ]
        [ options:
            - <string>
        ]
        [ sshOptions:
            - <string>
        ]
        [ user: <string> ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.source` | **必填** | object | 必填含 resource 的來源描述，此處無檔案路徑覆寫。 |
| `spec.source.resource` | **必填** | string | 必填 DHCPv6Server 裸名或 DHCPv6Server/name。使用其非空 leaseFile，否則為 <StateDir>/dnsmasq/dnsmasq.leases。並非狀態來源，無 field/optional。 |
| `spec.command` | *選填* | string | 選用執行檔名稱或路徑，預設 rsync；直接接收 rsync 引數，不是殼層命令字串。 |
| `spec.interval` | *選填* | string | Go duration，預設 30s。距上次成功 syncedAt 未達期間則略過；控制器另以 30 秒週期及訂閱事件執行。零或負值僅停用略過，不會加快週期排程。 |
| `spec.targets` | **必填** | array | 必填非空對等目標清單，逐一傳送檔案，第一個失敗記錄 Error/SyncFailed，不呼叫遠端 routerd API。 |
| `spec.targets[].name` | *選填* | string | 選用狀態標籤，預設空，不用於選擇目的主機或來源。 |
| `spec.targets[].host` | **必填** | string | 必填 SSH 主機或位址，不可含 CR/LF，須事先具備可達路由及非互動 SSH 認證。 |
| `spec.targets[].user` | *選填* | string | 選用 SSH 使用者，空值省略 user@ 並由 SSH 設定決定，不可含 CR/LF。 |
| `spec.targets[].sshOptions` | *選填* | array | 選用 SSH 引數清單，組合至 rsync -e。未指定對應鍵時補上 -o BatchMode=yes 與 -o ConnectTimeout=10，項目不可含 CR/LF。 |
| `spec.targets[].options` | *選填* | array | 選用額外 rsync 引數，附於預設之後。提供 --timeout 或 --timeout=... 時不補入 --timeout=60，但不變更獨立的 2 分鐘程序限制；不可含 CR/LF。 |
| `spec.when` | *選填* | object | 選用共用狀態條件，預設無條件。應以實際主用角色狀態避免備用端寫入，本資源不自行協商租約檔擁有者。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`
