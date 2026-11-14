# RouterdCluster

[各資源的設定](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

透過檔案租約與OS檔案鎖協調哪個routerd可以變更狀態，不是VRRP選舉或對端網路探索。

## 指定條件與參照關係

- 參與者需要共享且鎖定語意可靠的leasePath。peers是識別而非端點；租約實作只用非空清單啟用協調，不連線對端或驗證成員資格。待命controller以dry-run變更路徑運作。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    [ identity: <string> ]
    leasePath: <string>
    [ leaseTTL: <string> ]
    peers:
      - <string>
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
| `spec.peers` | **必填** | array | 去除空白與重複後至少兩項的必填清單，無預設值；不是Kind/名稱參照或實際連線位址。 |
| `spec.leaseTTL` | *選填* | string | 租約期限預設30s，明確值須為5s–10m的Go duration。互斥變更仍由鎖控制，期限本身不是網路故障偵測器。 |
| `spec.leasePath` | **必填** | string | 必填租約檔絕對路徑，不可含換行，無預設值；各主機獨立檔案不會協調主機間狀態。 |
| `spec.identity` | *選填* | string | 本機租約持有者識別，省略使用修剪後os.Hostname()，不是Router.metadata.name。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制，請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `leader`, `leaseExpiresAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
