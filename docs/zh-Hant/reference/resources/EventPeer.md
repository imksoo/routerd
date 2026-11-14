# EventPeer

[各資源的設定](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

宣告 EventGroup 的一個遠端傳送目標。傳輸驗證與本機識別由群組提供。

## 指定條件與參照關係

- groupRef、nodeName 不可為空，endpoint 必填。groupRef 比對 EventGroup 的單純 metadata.name，不是 EventGroup/name。EventPeer 驗證不檢查其存在；找不到群組就不加入配送設定。
- 靜態節點依 nodeName 覆寫匯入節點；同名靜態項目以後者為準。types 與 subjectPrefixes 兩類皆須符合，各清單內為 OR。區分大小寫；types 使用完整字串，不是主題萬用字元。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 選填; string; enum=["push"]
    endpoint: "<string>" # 選填; string
    groupRef: "<string>" # 必填; string
    nodeName: "<string>" # 必填; string
    subjectPrefixes: # 選填; array
      - "<string>" # string
    types: # 選填; array
      - "<string>" # string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.groupRef` | 必填的單純 EventGroup 名稱，對等傳輸需要群組的 auth.secretFile。無預設值。 |
| `spec.nodeName` | 必填遠端識別，也是去重與配送紀錄鍵。YAML 名稱不同但 nodeName 相同時，不會成為獨立節點。 |
| `spec.endpoint` | 必填基底 URL，例如 http://192.0.2.10:8787。推送端移除尾端斜線後附加 /v1/events，請勿自行加入此路徑。本機 YAML 驗證不檢查 URL、協定或可達性。 |
| `spec.direction` | 僅接受 push 或空，預設 push。產生的是向外推送目標，不是拉取用戶端。 |
| `spec.types` | 選用的事件型別完整比對允許清單，預設空允許全部型別。指定項目不可為空。 |
| `spec.subjectPrefixes` | 選用、區分大小寫的 subject 前綴清單。預設空允許全部 subject；符合任一前綴即可通過此類條件。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
