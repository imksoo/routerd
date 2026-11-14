# BFD

[各資源的設定](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過 vtysh 設定及觀察 FRR bfdd 工作階段，供 BGPPeer 故障處理使用。與 GoBGP 為不同後端，BFD Down 可觸發 BGP 對等端重設。

## 指定條件與參照關係

- 每個已宣告 BFD 至少須由一個 BGPPeer.bfd 參照，BFD peer 必須符合該 BGPPeer 名稱或其對等端位址。BGPDynamicPeer 沒有 bfd 欄位。
- 支援 Linux/FreeBSD，需有 FRR bfdd/vtysh。無介面時為多跳，使用參照 BGPRouter.routerID 作為本機位址；直接 IP 對等端且無介面時要求恰有一個 BGPRouter。指定介面則為單跳。
- BFD 僅偵測自身工作階段故障，Up 不代表所有路由目的地可達。GoBGP 在先前 BFD Up 或 BGP 已建立後觀察到 Down 才處理，不將所有未知或啟動狀態視為 Down。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 選填; integer; minimum=1; maximum=50
    interface: "<string>" # 選填; string
    minRx: "<string>" # 選填; string
    minTx: "<string>" # 選填; string
    peer: "<string>" # 必填; string
    profile: "" # 選填; string; enum=["", "fast", "normal", "slow"]
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
| `spec.peer` | 必填純 IP 位址或 BGPPeer/name 參照。BGPPeer 參照展開為其所有對等端位址，並從 routerRef 的 routerID 取得本機位址。直接對等端不接受主機名稱。 |
| `spec.interface` | 選用單跳介面。建議 Interface/name，控制器會解析宣告的 Interface.ifname；單獨名稱雖驗證為邏輯介面，卻會原樣傳給 FRR。省略使用多跳。 |
| `spec.profile` | fast 或省略：minRx=minTx=300ms、倍數 3；normal：500ms/500ms/3；slow：1000ms/1000ms/5。明確個別設定優先。 |
| `spec.minRx` | 選用 Go duration 50ms–60000ms，覆寫設定的接收間隔並轉為整數毫秒。省略使用 profile。這是 BFD 協定計時器，不是路由器重新啟動期限。 |
| `spec.minTx` | 選用 Go duration 50ms–60000ms，覆寫 profile 傳送間隔，輸出為整數毫秒。 |
| `spec.detectMultiplier` | 選用整數 1–50；解碼為 0 或省略時使用 profile 預設值（3，slow 為 5）。為協商偵測間隔倍數，不是 routerd 調整嘗試次數。 |
| `spec.when` | 選用的共用啟用條件，省略表示無條件。state/all/any、參照驗證與結構描述限制請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `peer`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
