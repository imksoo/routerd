# SAMSubnetPolicy

[各資源的設定](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

將分片指派發布為聯邦事件，讓各節點將 MobilityPool 所有權探索限縮至負責的 IPv4 子網。這是探索範圍指派，不是 IP 路由資源。

## 指定條件與參照關係

- sourcePrefix 及所有分片須為 IPv4 CIDR。分片須位於 sourcePrefix 內且不可重疊，但不要求完整涵蓋來源前綴。每個分片至少需一個非空且不重複的指派節點。
- 指派事件 10 分鐘後過期，由調整程序更新。指派缺少、過期或事件儲存區讀取錯誤時，不套用分片篩選，而非拒絕全部。不可僅以此資源作為授權邊界。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    poolRef: <string>
    shards:
      -
        assignedNodes:
          - <string>
        prefix: <string>
    sourcePrefix: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.sourcePrefix` | **必填** | string | 必填 IPv4 CIDR，限制所有分片範圍，無預設值。用於包含驗證，不套用為位址或路由。 |
| `spec.poolRef` | **必填** | string | 必填 MobilityPool metadata.name，作為事件酬載精確比對鍵，不是 Kind/name。目前驗證只檢查非空，不確認資源存在，須符合使用該事件的集區名稱。 |
| `spec.groupRef` | **必填** | string | 必填 EventGroup 參照，供發布及讀取指派使用。執行時須能解析群組，結構驗證僅檢查非空。請與目標 MobilityPool.groupRef 使用相同參照拼寫。 |
| `spec.shards` | **必填** | array | 必填且不可為空的不重疊子網及負責 nodeRef 清單。同一節點可出現在不同分片。 |
| `spec.shards[].prefix` | **必填** | string | 必填且位於 sourcePrefix 內的 IPv4 CIDR，可等於整個來源前綴。拒絕分片間重複或重疊。 |
| `spec.shards[].assignedNodes` | **必填** | array | 必填且不可為空的 nodeRef 清單，同一分片內不可重複。目前不檢查 SAMNodeSet 成員資格，值須符合消費端 EventGroup 本機節點識別。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。
