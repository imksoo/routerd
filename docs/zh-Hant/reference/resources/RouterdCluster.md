# RouterdCluster

[各資源的設定](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

透過檔案租約與OS檔案鎖協調哪個routerd可以變更狀態，不是VRRP選舉或對端網路探索。

## 指定條件與參照關係

- 參與者需要共享且鎖定語意可靠的leasePath。peers是識別而非端點；租約實作只用非空清單啟用協調，不連線對端或驗證成員資格。待命controller以dry-run變更路徑運作。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 選填; string
    leasePath: "<string>" # 選填; string
    leaseTTL: "<string>" # 選填; string
    peers: # 必填; array
      - "<string>" # string
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
| `spec.peers` | 去除空白與重複後至少兩項的必填清單，無預設值；不是Kind/名稱參照或實際連線位址。 |
| `spec.leaseTTL` | 租約期限預設30s，明確值須為5s–10m的Go duration。互斥變更仍由鎖控制，期限本身不是網路故障偵測器。 |
| `spec.leasePath` | 必填租約檔絕對路徑，不可含換行，無預設值；各主機獨立檔案不會協調主機間狀態。 |
| `spec.identity` | 本機租約持有者識別，省略使用修剪後os.Hostname()，不是Router.metadata.name。 |
| `spec.when` | 啟用條件，省略不設限制，請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
