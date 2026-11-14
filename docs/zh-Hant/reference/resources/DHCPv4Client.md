# DHCPv4Client

[各資源的設定](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

執行受管routerd-dhcpv4-client daemon，套用租約位址，以及選用的預設路由與主機DNS設定。

## 指定條件與參照關係

- interface須存在於宣告Router。識別欄位作為協定選項傳送，不是資源參照。租約到期/Release會移除已套用位址、路由及主機DNS；useRoutes/useDNS不停止租約取得。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # 選填; string
    clientID: "<string>" # 選填; string
    hostname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    requestedAddress: "<string>" # 選填; string
    routeMetric: 0 # 選填; integer; minimum=0
    useDNS: true # 選填; boolean
    useRoutes: true # 選填; boolean
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
| `spec.interface` | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.hostname` | DHCP選項12主機名稱，預設空即不傳送，不自動使用OS主機名稱，拒絕空白字元。 |
| `spec.requestedAddress` | 選填偏好IPv4位址，預設未指定，須為不含前綴長度的IPv4。以選項50請求，不保證分配，也不是靜態設定。 |
| `spec.classID` | 以選項60傳送的vendor class識別字串位元組，預設空則省略，不做十六進位解碼。 |
| `spec.clientID` | 選項61用戶端識別，明確值按字串位元組傳送，不解碼十六進位；預設為位元組1加介面硬體位址。伺服器以client ID識別租約時應保持穩定。 |
| `spec.useRoutes` | 預設true，租約有defaultGateway時套用預設路由。false移除先前受管預設路由，但保留租約取得與位址套用，也不隱藏狀態中的gateway。 |
| `spec.useDNS` | 預設true；/etc/resolv.conf指向systemd-resolved時用resolvectl，否則寫resolv.conf。false清除先前受管DNS，但狀態DNS仍可供DNSResolver相關參照。 |
| `spec.routeMetric` | 預設0，用於預設路由metric及平台特定位址協調。Schema要求非負整數，不是路由表ID。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
