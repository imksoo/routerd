# DHCPv4Client

[各資源的設定](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

執行受管routerd-dhcpv4-client daemon，套用租約位址，以及選用的預設路由與主機DNS設定。

## 指定條件與參照關係

- interface須存在於宣告Router。識別欄位作為協定選項傳送，不是資源參照。租約到期/Release會移除已套用位址、路由及主機DNS；useRoutes/useDNS不停止租約取得。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    [ classID: <string> ]
    [ clientID: <string> ]
    [ hostname: <string> ]
    interface: <string>
    [ requestedAddress: <string> ]
    [ routeMetric: <integer> ]
    [ useDNS: <boolean> ]
    [ useRoutes: <boolean> ]
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
| `spec.interface` | **必填** | string | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.hostname` | *選填* | string | DHCP選項12主機名稱，預設空即不傳送，不自動使用OS主機名稱，拒絕空白字元。 |
| `spec.requestedAddress` | *選填* | string | 選填偏好IPv4位址，預設未指定，須為不含前綴長度的IPv4。以選項50請求，不保證分配，也不是靜態設定。 |
| `spec.classID` | *選填* | string | 以選項60傳送的vendor class識別字串位元組，預設空則省略，不做十六進位解碼。 |
| `spec.clientID` | *選填* | string | 選項61用戶端識別，明確值按字串位元組傳送，不解碼十六進位；預設為位元組1加介面硬體位址。伺服器以client ID識別租約時應保持穩定。 |
| `spec.useRoutes` | *選填* | boolean | 預設true，租約有defaultGateway時套用預設路由。false移除先前受管預設路由，但保留租約取得與位址套用，也不隱藏狀態中的gateway。 |
| `spec.useDNS` | *選填* | boolean | 預設true；/etc/resolv.conf指向systemd-resolved時用resolvectl，否則寫resolv.conf。false清除先前受管DNS，但狀態DNS仍可供DNSResolver相關參照。 |
| `spec.routeMetric` | *選填* | integer; minimum=0 | 預設0，用於預設路由metric及平台特定位址協調。Schema要求非負整數，不是路由表ID。 |
| `spec.when` | *選填* | object | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
