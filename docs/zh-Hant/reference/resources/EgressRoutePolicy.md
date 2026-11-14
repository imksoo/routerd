# EgressRoutePolicy

[各資源的設定](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

選擇或設定出口路由，實際行為取決於明確 mode 與 hostTraffic。

## 指定條件與參照關係

- 請明確選擇路徑：省略 mode 僅公布建議選擇，Linux priority/mark/hash 安裝 IPv4 路由表與 nftables，hostTraffic true 為獨立本機 IPv6 路徑。即使結構描述接受，這些 IPv4 寫入器未實作一般 IPv6 轉送策略。
- 有效的直接 Linux 導向須有 interface/device/deviceFrom 輸出，以及正 table/priority/mark 組合。驗證允許三者皆零，但即時直接表安裝會略過，Applied 或選擇狀態不能證明封包導向。設定 metric 零時 Linux 路由用 50。
- priority 選就緒候選最大 weight，以較小 priority、名稱、輸入順序破同分。檢查新鮮健康、實際裝置、直接候選相依與 DS-Lite Up。目標池須至少一個健康且存在的目標，但此 priority 分支不評估候選 dependsOn。無可用者為 Pending/NoReadyCandidates 並移除新流導向，不保證所有探測路由或既有狀態撤除。
- mark 依資源與候選順序寫入各合格直接候選標記，重疊匹配可被後規則覆寫，不選單一最大 weight。hash/priority 目標池用位址 jhash、固定 256 桶 rendezvous 映射及 conntrack 標記，無目標權重可設，變更後既有流仍可保留標記。
- 共用 device/gateway 來源須有既有資源與明確提供欄位。DHCP 閘道不自動探索，須給 gatewayFrom。table/routeTable、routeMetric/metric 不可皆非零。targets 與直接 device/gateway/table/mark/metric 互斥，僅明確 hash/priority 可用；雖驗證僅在 hash 要求，產生仍需 hashFields。
- Linux 目標表設定檢查實際鏈路並略過缺失目標，但後續 hash 映射就緒副本對一般非 DS-Lite 目標只檢查健康，不重複檢查鏈路，因此缺失裝置仍可能收到新 hash 桶。依賴分配前須確認目標鏈路與健康。
- FreeBSD 僅支援 mode hash、family ipv4、hashFields: [sourceAddress]、非空 sourceCIDRs、單一啟用候選及至少兩個相異固定 IPv4 介面/閘道目標。省略 selection/hysteresis 與候選直接、動態、健康、相依欄位；目標 table/priority/mark/metric 須零，禁止健康與動態閘道。禁用目的集合參照，最多一個排除 CIDR，包含與排除不可並用。PF 用 route-to round-robin sticky-address，不是 Linux 決定性 hash。
- FreeBSD 轉譯器拒絕此 route-to 與明確 FirewallRule、產生的防火牆孔洞、FirewallEventLog 或 ClientPolicy 混用。若有 FirewallZone 過濾，須有廣泛允許轉送路徑；語法驗證不證明可與其他資源共同產生最終 PF 規則。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates:
      -
        [ dependsOn:
            -
              [ equals: <string> ]
              [ field: <string> ]
              [ notEmpty: <boolean> ]
              [ optional: <boolean> ]
              [ phase: <string> ]
              resource: <string>
        ]
        [ device: <string> ]
        [ deviceFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ enabled: <boolean> ]
        [ gateway: <string> ]
        [ gatewayFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ gatewaySource: ("" | "static" | "dhcpv4" | "dhcpv6" | "none") ]
        [ healthCheck: <string> ]
        [ interface: <string> ]
        [ mark: <integer> ]
        [ metric: <integer> ]
        [ name: <string> ]
        [ preferredSource: ("" | "interface") ]
        [ priority: <integer> ]
        [ routeMetric: <integer> ]
        [ routeTable: <integer> ]
        [ source: <string> ]
        [ table: <integer> ]
        [ targets:
            -
              [ gateway: <string> ]
              [ gatewayFrom:
                  field: <string>
                  [ optional: <boolean> ]
                  resource: <string>
              ]
              [ gatewaySource: ("" | "static" | "dhcpv4" | "dhcpv6" | "none") ]
              [ healthCheck: <string> ]
              [ interface: <string> ]
              [ mark: <integer> ]
              [ metric: <integer> ]
              [ name: <string> ]
              [ outboundInterface: <string> ]
              [ priority: <integer> ]
              [ routeMetric: <integer> ]
              [ routeTable: <integer> ]
              [ table: <integer> ]
        ]
        [ weight: <integer> ]
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
    [ destinationCIDRs:
        - <string>
    ]
    [ destinationSetRefs:
        - <string>
    ]
    [ excludeDestinationCIDRs:
        - <string>
    ]
    [ excludeDestinationSetRefs:
        - <string>
    ]
    [ family: ("ipv4" | "ipv6") ]
    [ hashFields:
        - <string>
    ]
    [ hostTraffic: <boolean> ]
    [ hysteresis: <string> ]
    [ mode: ("priority" | "mark" | "hash") ]
    [ selection: ("highest-weight-ready" | "weighted-ecmp") ]
    [ sourceCIDRs:
        - <string>
    ]
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
| `spec.family` | *選填* | string; enum=["ipv4", "ipv6"] | 預設 ipv4。結構描述與驗證接受 ipv6，但目前 Linux 轉送 mark/priority/hash 實作限 IPv4；IPv6 另有 hostTraffic 路徑，不代表一般 IPv6 轉送同等支援。FreeBSD 子集亦須 ipv4。 |
| `spec.hostTraffic` | *選填* | boolean | 預設 false。Linux true 須 family ipv6、單一候選、preferredSource: interface、固定 static gateway、mark 0 且無 targets。安裝所有本機流量的 iif lo 策略，不套用來源或目的 CIDR；裝置或位址未就緒保留先前有效規則，初次則 Pending。此獨立建構器不評估候選健康、相依或選擇欄位。 |
| `spec.mode` | *選填* | string; enum=["priority", "mark", "hash"] | 省略或空值僅做建議性狀態選擇，不安裝路由表。priority 選一個就緒候選供新流標記，mark 直接標記封包，hash 將流量映射到候選 targets。Linux 轉送請明確設定 mode；FreeBSD 僅限受限 hash 形狀。 |
| `spec.sourceCIDRs` | *選填* | array | 選用同位址族來源前綴，預設任意來源，清單為 OR。FreeBSD hash 需非空 IPv4 清單；hostTraffic 不使用此選擇器。 |
| `spec.destinationCIDRs` | *選填* | array | 選用同位址族目的前綴，與 destinationSetRefs 皆空時為全部目的。mark/hash 中與集合參照 OR 結合；priority 預設標記產生僅使用 CIDR，不使用位址集合。 |
| `spec.destinationSetRefs` | *選填* | array | 選用既有 IPAddressSet/<name> 參照，拒絕重複。用於 Linux mark/hash 匹配，priority 預設標記不套用，FreeBSD 禁用；空值不增加集合替代項。 |
| `spec.excludeDestinationCIDRs` | *選填* | array | 選用同位址族目的排除，預設空，須避開所有排除。IPv4 /0 排除不產生導向匹配。FreeBSD 最多一項，且不可與非空 destinationCIDRs 並用。 |
| `spec.excludeDestinationSetRefs` | *選填* | array | 選用既有 IPAddressSet/<name> 排除，拒絕重複，用於 Linux mark/hash，priority 預設標記忽略，FreeBSD 禁用。 |
| `spec.hashFields` | **條件必填** | array | sourceAddress/destinationAddress 的有序清單，hash 必填；含 priority 目標池在內，任何有 targets 的產生也需要。Linux 對位址而非埠做 jhash，使用資源衍生固定種子與 256 個 rendezvous 映射桶。FreeBSD 必須 [sourceAddress]，實作卻是 round-robin sticky-address 而非 jhash。 |
| `spec.selection` | *選填* | string; enum=["highest-weight-ready", "weighted-ecmp"] | 預設 highest-weight-ready，就緒候選依 weight 降序、priority 升序、有效名稱字典序、輸入順序選擇。weighted-ecmp 雖可通過結構描述，建議或 priority 選擇未實作，為 Pending/UnsupportedSelection。與 hash 目標演算法不同，mark/hash 不用此選項；FreeBSD 須空。 |
| `spec.hysteresis` | *選填* | string | Go duration，僅省略 mode 的建議選擇預設 30s。變更後此期間可保留仍就緒的舊勝者，不保留未就緒者；零或負值無延遲。明確 priority/mark/hash 不使用，FreeBSD 須空。 |
| `spec.candidates` | **必填** | array | 必填非空清單，停用項目仍做結構驗證。Linux priority 可混合直接候選與目標池；未驗證候選名稱唯一，請明確使用唯一名稱以免狀態或 hash 身分混淆。 |
| `spec.when` | *選填* | object | 選用共用狀態條件，預設無條件。為假時自有效設定移除，候選條件另行篩選；未解析可維持不確定，不保證停止。 |
| `spec.candidates[].name` | *選填* | string | 選用明確身分。建議模式依 source、candidate-N（零起算）補名；明確 priority 狀態依 interface/device、targets，未命名 hash 池用輸入索引。穩定身分請用唯一名稱。 |
| `spec.candidates[].enabled` | *選填* | boolean | 預設 true，false 排除選擇與轉譯，但不略過設定或參照驗證，也不單獨保證全部既有連線或路由表項消失。 |
| `spec.candidates[].source` | *選填* | string | 選用 Kind/name 建議就緒來源，不自動供應 device/gateway；此字串不受共用型別參照存在驗證。建議模式接受 Applied/Bound/Healthy/Installed/Ready/Running/Up，新鮮通過的健康檢查可替代未就緒來源。明確路由特別要求所參照 DS-Lite 為 Up，但不一般檢查其他來源。 |
| `spec.candidates[].interface` | **條件必填** | string | 明確 Linux 路由的選用已宣告邏輯介面，優先於 device，提供時驗證 Router 介面或 DS-Lite 索引。省略 mode 的建議輸出不使用此別名，該處請用 device/deviceFrom。targets 非空時禁用。 |
| `spec.candidates[].device` | **條件必填** | string | 選用固定裝置替代值，明確路由先用 interface，建議模式直接用 device。此字串無共用存在檢查，但即時安裝與選擇須有實際鏈路。已移除 ${...}，請用 deviceFrom；與 targets 禁止並用。 |
| `spec.candidates[].deviceFrom` | **條件必填** | object | 選用共用 resource/field 狀態來源，驗證既有資源與提供欄位。第一個正規化值優先於 interface/device，缺值退回固定值；optional 不建立裝置，不可與 targets 並用。 |
| `spec.candidates[].gateway` | **條件必填** | string | 固定純位址須符合 family，需要 gatewaySource: static 且與 gatewayFrom 互斥；DHCP 模式禁止固定值，預設空，目標池候選亦禁用。 |
| `spec.candidates[].gatewayFrom` | **條件必填** | object | 選用既有 resource/提供欄位來源。static 恰需 gateway/gatewayFrom 之一；DHCP 模式可用此來源，但不自動從介面探索閘道。建議模式任何未解析來源即使 optional true 仍未就緒；明確安裝在 static/DHCP 閘道缺值時報錯，optional 不是一般略過機制。與 targets 禁止並用。 |
| `spec.candidates[].gatewaySource` | *選填* | string; enum=["", "static", "dhcpv4", "dhcpv6", "none"] | 預設 none，為裝置預設路由並禁止 gateway 欄位。static 需固定或來源恰一個；dhcpv4 需 IPv4、無固定值且明確 gatewayFrom 可用。dhcpv6 雖僅在 IPv6 驗證接受，一般 IPv4 安裝器不支援，獨立 hostTraffic 亦須 static。池候選禁用。 |
| `spec.candidates[].preferredSource` | *選填* | string; enum=["", "interface"] | 空值或 interface，hostTraffic 須 interface，選候選介面第一個非 tentative/deprecated 的全域 IPv6。一般 IPv4 轉送及目標池不使用。 |
| `spec.candidates[].table` | **條件必填** | integer; minimum=0; maximum=4294967295 | Linux 路由表編號，結構描述 0–4294967295，預設 0 未設定。與非零 routeTable 互斥；有效直接導向需正 table/priority/mark（hostTraffic mark 為 0）。targets 候選禁用，FreeBSD 須 0。 |
| `spec.candidates[].routeTable` | **條件必填** | integer; minimum=0; maximum=4294967295 | table 為 0 時使用的別名，兩者非零拒絕；範圍及要求同 table。 |
| `spec.candidates[].priority` | **條件必填** | integer; minimum=0; maximum=32765 | 設定直接 mark/table/priority 任一時，Linux ip rule 優先序須 1–32765；預設 0 不是有效程式化規則。亦為候選選擇較小優先的同分判定，池候選可不設直接 table/mark 而使用；FreeBSD 須 0。 |
| `spec.candidates[].mark` | **條件必填** | integer; minimum=0; maximum=4294967295 | Linux 封包/fwmark 識別碼，結構描述 0–4294967295。直接轉送需正 mark/table/priority，預設 0 會略過直接路由表安裝。hostTraffic 須 0，池候選及 FreeBSD 禁止非零直接 mark。 |
| `spec.candidates[].routeMetric` | *選填* | integer; minimum=0 | 非負優先路由 metric，非零時與非零 metric 互斥；有效 0 在 Linux 安裝含 hostTraffic 時轉為 50，建議狀態仍可能回報 0。targets 禁用，FreeBSD 須 0。 |
| `spec.candidates[].metric` | *選填* | integer; minimum=0 | routeMetric 為 0 時的替代別名，兩者非零不可並用，Linux 程式化路由有效預設亦為 50。 |
| `spec.candidates[].weight` | *選填* | integer; minimum=0 | 非負整數預設 0，建議或 priority 選擇較大優先，不是百分比、ECMP 或目標 hash 權重。mark/hash 不依此比例分配，FreeBSD 須 0。 |
| `spec.candidates[].healthCheck` | *選填* | string | 選用既有 HealthCheck 裸名，空值無健康閘門。選擇須新鮮 Healthy 或未達 unhealthyThreshold 的 Failing，lastCheckedAt 不超過 max(2m,3×interval+timeout)。同 mark 探測的檢查可在未就緒時仍安裝探測表，與流量資格不同。hostTraffic 忽略，FreeBSD 禁用。 |
| `spec.candidates[].targets` | **條件必填** | array | 選用非空目標池，僅限 hash/priority。與直接 interface/device/deviceFrom/gateway/gatewayFrom/gatewaySource/table/routeTable/mark/routeMetric/metric 互斥。Linux 需各目標正 table/priority/mark 及 hashFields；FreeBSD 需單一候選、至少兩個相異固定 routehost 且無 Linux 路由數字。 |
| `spec.candidates[].dependsOn` | *選填* | array | 選用共用 AND 相依，預設空，套用一般參照檢查。建議、明確直接路由及 mark/hash 候選篩選會評估；目前 priority 目標池資格不評估候選 dependsOn，hostTraffic 亦忽略。候選整體停止請用 when，FreeBSD 禁用。 |
| `spec.candidates[].when` | *選填* | object | 選用共用狀態條件，預設無條件；為假時在控制器選擇前移除，未解析可因不確定而保留，enabled false 亦移除。FreeBSD 固定子集禁止候選條件。 |
| `spec.candidates[].targets[].name` | *選填* | string | 選用目標身分，預設空，與有效 interface、mark、table 共同組成穩定 hash 身分，變更可重新分配新流。Linux 通常不驗證名稱/mark/priority/table 唯一。 |
| `spec.candidates[].targets[].interface` | *選填* | string | 未提供 outboundInterface 時必填目標邏輯介面，驗證已宣告介面或 DS-Lite 索引。Linux 同時提供時 outboundInterface 優先；FreeBSD 拒絕兩者非空。 |
| `spec.candidates[].targets[].outboundInterface` | *選填* | string | 優先於 interface 的別名，套用相同參照檢查；實際傳遞仍須可用 OS 介面，不建立隧道。 |
| `spec.candidates[].targets[].gateway` | *選填* | string | 選用符合 family 的固定值，須 gatewaySource static 且與 gatewayFrom 互斥；DHCP 模式禁止固定值。FreeBSD 目標必填此純固定 IPv4。 |
| `spec.candidates[].targets[].gatewayFrom` | *選填* | object | 選用共用 resource/field 來源，即使 optional true 仍驗證存在及提供欄位。static 恰需固定值或來源之一，Linux 使用第一個值，optional 不略過閘道缺值；FreeBSD 完全禁止動態閘道欄位。 |
| `spec.candidates[].targets[].gatewaySource` | *選填* | string; enum=["", "static", "dhcpv4", "dhcpv6", "none"] | 預設 none，禁止 gateway/gatewayFrom 並安裝裝置路由。static 恰需 gateway/gatewayFrom 之一；dhcpv4/6 需相應位址族且無固定值，但不自動探索，請用 gatewayFrom。一般即時安裝器僅支援 none/static/dhcpv4，FreeBSD 須明確 static。 |
| `spec.candidates[].targets[].table` | *選填* | integer; minimum=0; maximum=4294967295 | Linux 每目標必須正 table（結構描述上限 4294967295），與非零 routeTable 互斥；無自動配置、保留表或唯一檢查。FreeBSD 須省略或 0，改用 PF route-to。 |
| `spec.candidates[].targets[].routeTable` | *選填* | integer; minimum=0; maximum=4294967295 | table 為 0 時使用的別名，兩者非零拒絕，Linux 正值或 FreeBSD 零值要求相同。 |
| `spec.candidates[].targets[].priority` | *選填* | integer; minimum=0; maximum=32765 | Linux 目標 ip rule 優先序必填 1–32765，無自動配置，與候選選擇 priority 不同。FreeBSD 須省略或 0。 |
| `spec.candidates[].targets[].mark` | *選填* | integer; minimum=0; maximum=4294967295 | Linux 目標 fwmark 必填正值（結構描述上限 4294967295），用於 hash 桶與 ip rule，無自動配置或衝突偵測。FreeBSD 須省略或 0。 |
| `spec.candidates[].targets[].routeMetric` | *選填* | integer; minimum=0 | 非負 Linux 路由 metric，優先於 metric，兩者非零拒絕，有效省略或 0 安裝時為 50。FreeBSD 須省略或 0。 |
| `spec.candidates[].targets[].metric` | *選填* | integer; minimum=0 | routeMetric 為 0 時的替代別名，要求相同，Linux 有效預設 50。 |
| `spec.candidates[].targets[].healthCheck` | *選填* | string | 選用既有 HealthCheck 裸名，預設無健康閘門，同候選鮮度與閾值檢查。失敗目標自新 hash 映射排除，但既有 conntrack 標記會還原而非全面終止連線，同 mark 探測表亦可保留；FreeBSD 禁用。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/egressroute/selection.go](https://github.com/imksoo/routerd/blob/main/pkg/egressroute/selection.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
