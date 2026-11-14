# SAMNodeSet

[各資源的設定](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

宣告供傳輸、事件對等端、WireGuard 與 MobilityPool 成員解析共用的 SAM 節點身分與拓撲。本機供應者擷取/探索設定應放在 MobilityPool 自節點覆寫，不放在此處。

## 指定條件與參照關係

- nodes 不可為空，移除首尾空白後 nodeRef 不可重複。不從 nodeRef 推定 role/site；MobilityPool 等使用端另施加角色及站點要求。
- 設定任一 WireGuard 資料時，publicKey 及非空 allowedIPs 必填。結構驗證中 endpoint/transportEndpoint 仍選用，但須符合選用傳輸。samEndpoint 與 samEndpointFrom 互斥。
- placement 群組要求 role=cloud，且群組內 site 一致。priority、drain=true 及正數 maxSecondaryIPs 皆需非空 placement.group。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes:
      -
        [ eventEndpoint: <string> ]
        [ macAddresses:
            - <string>
        ]
        [ maintenance:
            [ drain: <boolean> ]
        ]
        [ maxSecondaryIPs: <integer> ]
        nodeRef: <string>
        [ placement:
            [ group: <string> ]
            [ priority: <integer> ]
        ]
        [ role: ("" | "onprem" | "cloud") ]
        [ routeReflector: <boolean> ]
        [ samEndpoint: <string> ]
        [ samEndpointFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ site: <string> ]
        [ wireGuard:
            [ allowedIPs:
                - <string>
            ]
            [ endpoint: <string> ]
            [ persistentKeepalive: <integer> ]
            [ publicKey: <string> ]
            [ transportEndpoint: <string> ]
        ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.nodes` | **必填** | array | 必填且不可為空的共用節點清單，不自動加入本機節點。 |
| `spec.nodes[].nodeRef` | **必填** | string | 必填穩定節點識別，在此 NodeSet 內唯一。使用端與 EventGroup 本機識別或 SAMTransportProfile.selfNodeRef 比對，不是 Kind/name 參照。 |
| `spec.nodes[].site` | *選填* | string | 選用站點標籤，預設為空，不由 role 衍生。MobilityPool 成員及 placement 規則可能要求明確且相容的 site。 |
| `spec.nodes[].role` | *選填* | string; enum=["", "onprem", "cloud"] | NodeSet 驗證接受空值/onprem/cloud，無推定預設值。MobilityPool 成員需實際 onprem/cloud 角色，placement 僅供 cloud。 |
| `spec.nodes[].routeReflector` | *選填* | boolean | 預設 false，標記節點可供註冊拓撲選為 RR 身分，不會單獨建立 BGPRouter 或 BGP 連線。 |
| `spec.nodes[].eventEndpoint` | *選填* | string | 供產生事件對等端使用的選用絕對 HTTP(S) URL，須含主機，無預設值或狀態來源。 |
| `spec.nodes[].samEndpoint` | *選填* | string | 選用的節點外層傳輸端點 IPv4 位址或 CIDR，不接受 DNS 名稱，與 samEndpointFrom 互斥。WireGuard 加密時優先使用 wireGuard.transportEndpoint。 |
| `spec.nodes[].samEndpointFrom` | *選填* | object | 遵循共用 resource/field 規則的選用執行時狀態來源。缺少值仍屬未解析傳輸來源，optional 不會補入端點或忽略格式錯誤位址。 |
| `spec.nodes[].samEndpointFrom.resource` | **條件必填** | string | 使用來源時必填的已宣告資源 Kind/name，與固定 samEndpoint 互斥。 |
| `spec.nodes[].samEndpointFrom.field` | **條件必填** | string | 指定 resource 時必填的明確提供狀態欄位，例如 DHCPv4Client 的 currentAddress。 |
| `spec.nodes[].samEndpointFrom.optional` | *選填* | boolean | 預設 false，不略過存在/欄位驗證；此傳輸使用端仍將端點缺少回報為未解析。 |
| `spec.nodes[].macAddresses` | *選填* | array | 選用 MAC 位址清單，預設為空。ARP 觀察意圖將其列為忽略的傳送端 MAC，避免將 SAM 路由器當用戶端重新探索；不是變更 MAC 的要求。 |
| `spec.nodes[].wireGuard` | *選填* | object | 選用公開 WireGuard 傳輸身分，任一子欄位非空時需 publicKey 與 allowedIPs。私密金鑰不應放入 NodeSet 拓撲。 |
| `spec.nodes[].wireGuard.publicKey` | *選填* | string | 設定 wireGuard 時必填的對等端公開金鑰，傳入產生的 WireGuardPeer，無衍生預設值。 |
| `spec.nodes[].wireGuard.endpoint` | *選填* | string | 選用 WireGuard 外層 UDP host:port，IPv6 使用 [IPv6]:port，埠 1–65535，與內層 transportEndpoint 不同。 |
| `spec.nodes[].wireGuard.transportEndpoint` | *選填* | string | encryption=wireguard 時選用的內層 SAM 隧道 IPv4 位址/CIDR，優先於 samEndpoint/samEndpointFrom，不是 UDP 通訊端位址。 |
| `spec.nodes[].wireGuard.allowedIPs` | *選填* | array | 設定 WireGuard 時必填且非空的 IPv4/IPv6 CIDR，傳入產生的對等端，不會為每個前綴自動建立一般路由。 |
| `spec.nodes[].wireGuard.persistentKeepalive` | *選填* | integer; minimum=0; maximum=65535 | 整數秒 0–65535，預設 0 停用持續保活，複製至產生的對等端。 |
| `spec.nodes[].placement` | *選填* | object | 選用共用擷取配置資訊，僅 cloud 角色可宣告群組；成員須有一致 site/role，使用端也須符合供應者身分相容性。 |
| `spec.nodes[].placement.group` | *選填* | string | 選用配置群組名稱，預設空值表示不群組選擇。priority、drain 或正容量設定時必填。 |
| `spec.nodes[].placement.priority` | *選填* | integer; minimum=0; maximum=1000000 | 整數 0–1000000。省略或 0 依解析成員順序指派 10、20…並避開明確值；單一作用中選擇優先較小的有效數值，非零需 group。 |
| `spec.nodes[].maintenance` | *選填* | object | 選用宣告式維護意圖，預設不 drain，是共用拓撲資訊，不是本機供應者憑證。 |
| `spec.nodes[].maintenance.drain` | *選填* | boolean | 預設 false。true 將群組成員排除於作用中擷取選擇之外，需 placement.group。 |
| `spec.nodes[].maxSecondaryIPs` | *選填* | integer; minimum=0 | 非負整數，預設 0。正值需 placement.group，啟用該群組依容量分散擷取；0 表示無個別成員容量上限，不變更雲端配額。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `nodeCount`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
