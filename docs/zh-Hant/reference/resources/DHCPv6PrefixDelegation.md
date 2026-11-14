# DHCPv6PrefixDelegation

[各資源的設定](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在上行介面取得 IPv6 委派前綴。下游或主機位址須以 IPv6DelegatedAddress 衍生並套用，僅取得前綴不會向客戶端公告。

## 指定條件與參照關係

- interface 須為既有邏輯介面名稱，預設 client 為 routerd-dhcpv6-client。亦識別 networkd、dhcp6c、dhcpcd 並產生平台設定，但即時監督與狀態通訊端以 routerd 常駐程式為主，生命週期支援不應視為等同。
- profile/prefixLength 影響外部客戶端設定及還原快照比對。受監督的 routerd-dhcpv6-client 不接收 profile 或 prefix-length 旗標，取得時不以此提示長度；clientDUID 會傳入。
- 不支援 iaid/duidType YAML 控制。routerd 常駐程式 IA_PD IAID 預設 1，未指定 clientDUID 時由上行 MAC 衍生 DUID-LL。required 不會實作阻擋開機的要求。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 選填; string
    clientDUID: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    prefixLength: 1 # 選填; integer; minimum=1; maximum=128
    profile: "default" # 選填; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 選填; boolean
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
| `spec.interface` | 必填同一 Router 的邏輯上行介面。若 DHCPv6Address 亦使用同一介面，外部 dhcp6c/dhcpcd PD client 須與該資源 client 相符。 |
| `spec.client` | 預設 routerd-dhcpv6-client，接受 networkd/dhcp6c/dhcpcd。目前監督亦可能合成 routerd DHCPv6 systemd 單元，選外部客戶端不普遍保證停用內建常駐程式。 |
| `spec.profile` | 空值/default、ntt-ngn-direct-hikari-denwa 或 ntt-hgw-lan-pd。NTT 在支援的產生路徑提供 /60 與 link-layer DUID 預設，不設定業者憑證或變更內建命令。 |
| `spec.prefixLength` | 選用 1–128，省略或 0 時 NTT 為 /60，default 無長度提示，不保證分配長度，也不傳入內建取得。HA 預置快照重用卻要求有效長度完全相同：default 省略時為 0，不符合一般委派前綴，此路徑須明確指定實際長度。 |
| `spec.clientDUID` | 選用無分隔符且偶數長度的十六進位 DUID，拒絕冒號與空白。空值由內建客戶端從 MAC 衍生 DUID-LL，身分亦影響還原租約是否可重用。 |
| `spec.required` | 布林值預設 false，會解碼但目前不作為必要性或開機等待策略。相依者應使用實際 Bound 狀態及相依條件。 |
| `spec.when` | 選用共用狀態條件，預設無條件。監督客戶端依已解析條件篩選，條件為假可在保留宣告時停止取得。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
