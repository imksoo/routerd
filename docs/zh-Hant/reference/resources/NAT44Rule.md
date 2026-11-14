# NAT44Rule

[各資源的設定](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過輸出介面或 EgressRoutePolicy 選定裝置轉換路由 IPv4 來源。NAT 本身不允許轉送，也不指派轉換位址。

## 指定條件與參照關係

- 執行控制器請使用 type + egressInterface/egressPolicyRef + sourceRanges。type 必須為 masquerade/snat；至少一個輸出選擇與一個 IPv4 來源範圍必填。兩種輸出選擇皆有時，egressInterface 優先。
- snat 必須且只能提供 snatAddress 或 snatAddressFrom 其中之一；masquerade 禁止兩者。動態來源遵循共用 resource/field 驗證，無可用位址時維持 Pending；optional 不會略過 SNAT 位址需求。
- 替代 outboundInterface/sourceCIDRs/translation 形式可通過驗證並出現在計畫，但執行 NAT44 控制器不會轉成渲染規則；Linux 可能維持 Pending 或產生錯誤而不套用。靜態 PF 僅支援此形式的 interfaceAddress/address，不支援 pool；portMapping 無產生效果。請勿視為可用執行設定。
- 兩種形式互斥。替代形式需 outboundInterface、非空 IPv4 sourceCIDRs 與 translation.type，禁止傳統 type/egress/sourceRanges/目的/SNAT 欄位。未使用的 translation.address/addresses 不互斥拒絕，但僅選定分支會使用。
- Linux 將 destinationCIDRs 與 destinationSetRefs 作 OR，再排除指定 CIDR/集合。PF 套用靜態目的與排除 CIDR，但忽略兩個位址集合參照清單。規則依 metadata.name 排序，不依 YAML 順序。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    [ destinationCIDRs:
        - <string>
    ]
    [ destinationSetRefs:
        - <string>
    ]
    [ egressInterface: <string> ]
    [ egressPolicyRef: <string> ]
    [ excludeDestinationCIDRs:
        - <string>
    ]
    [ excludeDestinationSetRefs:
        - <string>
    ]
    [ outboundInterface: <string> ]
    [ snatAddress: <string> ]
    [ snatAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ sourceCIDRs:
        - <string>
    ]
    [ sourceRanges:
        - <string>
    ]
    [ translation:
        [ address: <string> ]
        [ addresses:
            - <string>
        ]
        [ portMapping:
            [ end: <integer> ]
            [ start: <integer> ]
            [ type: ("auto" | "preserve" | "range") ]
        ]
        type: ("interfaceAddress" | "address" | "pool")
    ]
    [ type: ("masquerade" | "snat") ]
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
| `spec.type` | **條件必填** | string; enum=["masquerade", "snat"] | 執行形式必填：masquerade 使用輸出位址，snat 使用提供位址。無預設；不可搭配替代形式。 |
| `spec.egressInterface` | **條件必填** | string | 選用單純設定邏輯介面/通道名稱，指定時須在介面/DSLite 索引中存在，優先於 egressPolicyRef。 |
| `spec.egressPolicyRef` | **條件必填** | string | egressInterface 缺省時使用的單純 EgressRoutePolicy 名稱。需要 status.phase=Applied 與非空 selectedDevice。NAT44 驗證不檢查存在性，錯名或未解析會 Pending。 |
| `spec.sourceRanges` | **條件必填** | array | 執行形式必填非空 IPv4 CIDR OR 清單。不自動探索 LAN，與 sourceCIDRs/translation 形式互斥。 |
| `spec.destinationCIDRs` | *選填* | array | 選用 IPv4 目的 OR 清單，預設空。Linux 與 destinationSetRefs 取聯集；PF 使用靜態 CIDR。 |
| `spec.destinationSetRefs` | *選填* | array | 選用既有 IPAddressSet/<name> 參照，不可重複。預設空，Linux 有效，PF 忽略。 |
| `spec.excludeDestinationCIDRs` | *選填* | array | 選用從此 NAT 選擇排除的 IPv4 目的，預設空。PF 在此規則轉換前產生 no-nat。 |
| `spec.excludeDestinationSetRefs` | *選填* | array | 選用既有 IPAddressSet/<name> 排除，預設空。僅 Linux 有效，PF 忽略。 |
| `spec.snatAddress` | **條件必填** | string | type=snat 的純 IPv4 轉換位址，與 snatAddressFrom 必須且只能擇一。masquerade 禁用。不會指派位址。 |
| `spec.snatAddressFrom` | **條件必填** | object | 僅供 snat 的共用 resource/field/optional 來源。先讀狀態，再讀支援的宣告資源欄位；無可用值時 Pending。請指定提供 IPv4 位址的欄位。 |
| `spec.outboundInterface` | **條件必填** | string | 替代驗證形式必填的單純介面名稱；如上所述執行控制器不支援。不可搭配 egressInterface/egressPolicyRef。 |
| `spec.sourceCIDRs` | **條件必填** | array | 替代形式必填非空 IPv4 前綴，無預設。執行控制器不使用。 |
| `spec.translation` | **條件必填** | object | 替代形式的轉換物件，搭配 outboundInterface/sourceCIDRs。通過驗證與出現計畫不代表執行安裝。 |
| `spec.translation.type` | **條件必填** | string; enum=["interfaceAddress", "address", "pool"] | 替代形式必填 interfaceAddress/address/pool。靜態 PF 僅支援前兩者，執行控制器不套用此形式。 |
| `spec.translation.address` | **條件必填** | string | translation.type=address 時必填純 IPv4，預設空。其他型別不使用。 |
| `spec.translation.addresses` | **條件必填** | array | type=pool 時必填非空純 IPv4 清單。接受並規劃，但目前執行控制器與靜態 PF 未實作。 |
| `spec.translation.portMapping` | *選填* | object | 選用替代形式連接埠策略，僅驗證與規劃。目前 Linux 執行與 PF 不強制這些設定。 |
| `spec.translation.portMapping.type` | *選填* | string; enum=["auto", "preserve", "range"] | auto（預設）、preserve 或 range。auto/preserve 禁止 start/end，range 需要兩端。目前無產生效果。 |
| `spec.translation.portMapping.start` | *選填* | integer; minimum=1; maximum=65535 | 僅 range 必填起點，1–65535 且 <= end。預設 0 為未指定，目前僅驗證。 |
| `spec.translation.portMapping.end` | *選填* | integer; minimum=1; maximum=65535 | 僅 range 必填終點，1–65535 且 >= start。預設 0 為未指定，目前僅驗證。 |
| `spec.when` | *選填* | object | 選用共用啟用條件，預設無條件。未啟用或已篩除規則不納入有效 NAT 產生。條件與結構描述限制請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `egressInterface`, `snatAddress`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
