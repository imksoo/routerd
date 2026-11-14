# EventPeer

[各資源的設定](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

宣告 EventGroup 的一個遠端傳送目標。傳輸驗證與本機識別由群組提供。

## 指定條件與參照關係

- groupRef、nodeName 不可為空，endpoint 必填。groupRef 比對 EventGroup 的單純 metadata.name，不是 EventGroup/name。EventPeer 驗證不檢查其存在；找不到群組就不加入配送設定。
- 靜態節點依 nodeName 覆寫匯入節點；同名靜態項目以後者為準。types 與 subjectPrefixes 兩類皆須符合，各清單內為 OR。區分大小寫；types 使用完整字串，不是主題萬用字元。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    [ direction: ("push") ]
    endpoint: <string>
    groupRef: <string>
    nodeName: <string>
    [ subjectPrefixes:
        - <string>
    ]
    [ types:
        - <string>
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必填** | string | 必填的單純 EventGroup 名稱，對等傳輸需要群組的 auth.secretFile。無預設值。 |
| `spec.nodeName` | **必填** | string | 必填遠端識別，也是去重與配送紀錄鍵。YAML 名稱不同但 nodeName 相同時，不會成為獨立節點。 |
| `spec.endpoint` | **必填** | string | 必填基底 URL，例如 http://192.0.2.10:8787。推送端移除尾端斜線後附加 /v1/events，請勿自行加入此路徑。本機 YAML 驗證不檢查 URL、協定或可達性。 |
| `spec.direction` | *選填* | string; enum=["push"] | 僅接受 push 或空，預設 push。產生的是向外推送目標，不是拉取用戶端。 |
| `spec.types` | *選填* | array | 選用的事件型別完整比對允許清單，預設空允許全部型別。指定項目不可為空。 |
| `spec.subjectPrefixes` | *選填* | array | 選用、區分大小寫的 subject 前綴清單。預設空允許全部 subject；符合任一前綴即可通過此類條件。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
