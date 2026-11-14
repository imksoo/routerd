# SAMTransportProfile

[各資源的設定](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

從共用拓撲或接納的 RR/direct 快照衍生節點間 SAM 隧道、端點 /32 路由及選用 BGP/BFD。

## 指定條件與參照關係

- direct 要求必要 SAMRRSet 後的最後 direct SAMPeerGroup、pair-stable、產生 BGP、routeReflectorClient:false、IPv4 匯入及 min=max=32、低於 direct 的正 RR LOCAL_PREF，以及 BGPRouter.communities.set.out 的本節點識別 community。direct 不可用時保留 RR。
- 產生 RR 用戶端/direct 匯入強制 /32 及對等端識別 community，禁止其他已知節點識別。direct 前綴僅為該 leaf 接納擁有 /32，空值全部拒絕，並強制 nextHopRewrite=peer-address。direct 設定的 RR 備援下一跳也正規化為 peer-address。
- 無本機 MobilityPool 的 RR 用戶端需明確 IPv4 匯入前綴，長度省略或皆 32。FreeBSD 衍生 gifN/greN 與明確點對點 peerAddress；Linux 使用短隧道名稱。衍生成功不證明隧道/BGP 可達。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    [ addressingMode: ("" | "edge-index" | "pair-stable") ]
    bgp:
      [ bfd:
          [ detectMultiplier: <integer> ]
          [ enabled: <boolean> ]
          [ interface: <string> ]
          [ minRx: <string> ]
          [ minTx: <string> ]
          [ profile: ("" | "fast" | "normal" | "slow") ]
      ]
      [ convergenceProfile: ("" | "default" | "fast" | "stable") ]
      [ directLocalPreference: <integer> ]
      [ ebgpMultihop: <integer> ]
      [ exportPolicy:
          [ allowedPrefixes:
              - <string>
          ]
      ]
      [ generatePeers: <boolean> ]
      [ importPolicy:
          [ allowedPrefixLengthMax: <integer> ]
          [ allowedPrefixLengthMin: <integer> ]
          [ allowedPrefixes:
              - <string>
          ]
          [ forbiddenCommunities:
              - <string>
          ]
          [ localPreference: <integer> ]
          [ nextHopRewrite: ("" | "peer-address" | "unchanged") ]
          [ requiredCommunities:
              - <string>
          ]
      ]
      peerASN: <integer>
      [ routeReflectorClient: <boolean> ]
      [ routeReflectorClusterID: <string> ]
      routerRef: <string>
      [ timers:
          [ profile: ("" | "default" | "fast" | "slow") ]
      ]
      [ timersPreset: ("" | "default" | "fast" | "slow") ]
    [ encapDport: <integer> ]
    [ encapSport: <integer> ]
    [ encryption: ("" | "none" | "wireguard") ]
    innerPrefix: <string>
    [ localEndpoint: <string> ]
    [ localEndpointFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    mode: ("ipip" | "gre" | "fou" | "gue")
    [ peersFrom:
        -
          [ direct: <boolean> ]
          [ nodeRefs:
              - <string>
          ]
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ publishPeerGroup: <boolean> ]
    selfNodeRef: <string>
    underlayInterface: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.selfNodeRef` | **必填** | string | 必填穩定本機 nodeRef，不從主機名稱/router ID 推定。 |
| `spec.mode` | **必填** | string; enum=["ipip", "gre", "fou", "gue"] | 必填 ipip/gre/fou/gue。FreeBSD 僅支援 ipip/gif 與 gre，fou/gue 需 Linux。衍生隧道本身不加密。 |
| `spec.encryption` | *選填* | string; enum=["", "none", "wireguard"] | none/省略或 wireguard。wireguard 優先節點 wireGuard.transportEndpoint，不產生私密金鑰或本機 WireGuardInterface，須另設定 WireGuard 底層。 |
| `spec.innerPrefix` | **必填** | string | 必填長度 <=31 的 IPv4 CIDR，拆為 /31 邊，須有足夠拓撲插槽。pair-stable 衝突會報錯，不靜默移動端點。 |
| `spec.addressingMode` | *選填* | string; enum=["", "edge-index", "pair-stable"] | 預設 edge-index 依排序拓撲邊指派 /31，拓撲變更可能改變索引。pair-stable 對 innerPrefix 與無序節點配對雜湊至固定插槽，direct 來源必填。 |
| `spec.underlayInterface` | **必填** | string | 必填已宣告邏輯介面，用於衍生隧道及遠端端點 /32 裝置路由，衍生隧道設定 trustedUnderlay:true。 |
| `spec.localEndpoint` | **條件必填** | string | 本機外層 IPv4 固定值，與 localEndpointFrom 必須且只能指定一項，不自動選擇介面位址。 |
| `spec.localEndpointFrom` | **條件必填** | object | 取代固定 localEndpoint 的狀態來源，適用共用 resource/field 規則，未解析端點使相依傳輸待處理。 |
| `spec.localEndpointFrom.resource` | **條件必填** | string | 使用來源時必填既有 Kind/name。 |
| `spec.localEndpointFrom.field` | **條件必填** | string | 必填來源提供欄位，例如 currentAddress。 |
| `spec.localEndpointFrom.optional` | *選填* | boolean | 預設 false，不免除參照驗證，也不補入未解析端點。 |
| `spec.encapSport` | **條件必填** | integer; minimum=1; maximum=65535 | fou/gue 必填 UDP 來源埠 1–65535，ipip/gre 禁用（省略或 0）。 |
| `spec.encapDport` | **條件必填** | integer; minimum=1; maximum=65535 | fou/gue 必填 UDP 目的埠 1–65535，ipip/gre 禁用，無隱含預設埠。 |
| `spec.peersFrom` | **條件必填** | array | 有順序的拓撲來源，除 publishPeerGroup:true 外至少一個。自己不作為對等端但保留於共用拓撲，後續重複節點可能覆寫前方資料。 |
| `spec.peersFrom[].resource` | **條件必填** | string | 必填 SAMNodeSet/name、SAMPeerGroup/name、SAMEnrollmentPolicy/name 或 SAMRRSet/name。NodeSet 為靜態，執行時來源提供接納/共用快照。 |
| `spec.peersFrom[].optional` | *選填* | boolean | 預設 false，非 optional 來源缺少為待處理。direct 不可用/不相容不移除可用 RR 備援對等端。 |
| `spec.peersFrom[].direct` | *選填* | boolean | 預設 false，true 僅限必要 SAMRRSet 後的最後 SAMPeerGroup 來源，另適用下述 direct 限制。 |
| `spec.peersFrom[].nodeRefs` | *選填* | array | 選用唯一非空 nodeRef 選擇值，省略選取所有合格來源節點。僅縮小本機鄰接，不取代共用權威拓撲。 |
| `spec.publishPeerGroup` | *選填* | boolean | 預設 false。true 將本機端點發布為執行時 SAMPeerGroup/<設定 metadata.name>，允許 peersFrom 為空，不發布私密憑證。 |
| `spec.bgp` | **必填** | object | 必填產生對等端設定，所參照 BGPRouter 須已存在。 |
| `spec.bgp.generatePeers` | *選填* | boolean | 預設 true，false 僅產生傳輸不產生 BGP/BFD，direct 來源時禁用。 |
| `spec.bgp.timersPreset` | *選填* | string; enum=["", "default", "fast", "slow"] | 選用 default/fast/slow 簡寫，優先序低於 bgp.timers.profile，兩者皆空時產生對等端繼承 BGPRouter.timers。 |
| `spec.bgp.directLocalPreference` | *選填* | integer | direct 對等端 uint32 LOCAL_PREF，省略或 0 預設 200，direct 時須大於正 RR bgp.importPolicy.localPreference。 |
| `spec.bgp.bfd` | *選填* | object | 選用產生 BFD 設定，僅 generatePeers 與 bfd.enabled 皆 true 時使用。 |
| `spec.bgp.bfd.enabled` | *選填* | boolean | 預設 false。true 衍生連結產生 BGPPeer 的 BFD 資源，需 FRR bfdd/vtysh。 |
| `spec.bgp.routerRef` | **必填** | string | 必填 BGPRouter/name，參照同一 Router 設定中宣告的路由器，不接受單獨名稱。 |
| `spec.bgp.peerASN` | **必填** | integer; minimum=1 | 必填非零 uint32 遠端 ASN，等於本機 ASN 時為 iBGP；routeReflectorClient 要求兩者相等。 |
| `spec.bgp.ebgpMultihop` | *選填* | integer; minimum=0; maximum=255 | 整數 0–255，預設 0。僅大於 1 時啟用 GoBGP eBGP multihop 並設定 TTL，0/1 保持停用。 |
| `spec.bgp.routeReflectorClient` | *選填* | boolean | 預設 false。true 標記 iBGP 路由反射器用戶端，要求 peerASN 等於路由器 ASN，並將路由器匯入前綴加入有效匯出前綴。 |
| `spec.bgp.routeReflectorClusterID` | *選填* | string | 選用 IPv4 格式叢集 ID，僅 routeReflectorClient=true 時傳入。空值由 GoBGP 決定，單獨設定不啟用反射。 |
| `spec.bgp.convergenceProfile` | *選填* | string; enum=["", "default", "fast", "stable"] | 選用 default/fast/stable，省略繼承 BGPRouter convergenceProfile。選擇 graceful restart 預設值，不是 timers.profile；路由器 gracefulRestart.enabled 仍優先覆寫。 |
| `spec.bgp.importPolicy` | *選填* | object | 選用匯入原則。對等端若無非空前綴、community 條件、下一跳改寫或 localPreference，會完整繼承路由器原則；僅前綴長度限制不算覆寫。 |
| `spec.bgp.importPolicy.allowedPrefixes` | *選填* | array | 選用 IPv4/IPv6 CIDR 允許清單，依前綴及允許的更細前綴長度比對。拒絕遮罩後重複的前綴。為空時，不在其他已設定匯入條件上加入前綴條件。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMin` | *選填* | integer; minimum=0; maximum=128 | 整數 0–128。省略或 0 使用每個允許前綴自身長度。正值須大於等於各前綴長度、小於等於家族上限及正數最大長度。 |
| `spec.bgp.importPolicy.allowedPrefixLengthMax` | *選填* | integer; minimum=0; maximum=128 | 整數 0–128。省略或 0 時 IPv4 為 32、IPv6 為 128。正值須涵蓋所有列出前綴長度且不超過家族上限。動態 leaf 接納要求 min=max=32。 |
| `spec.bgp.importPolicy.requiredCommunities` | *選填* | array | 選用 community 清單；接受的路徑必須包含全部列出值。空清單不增加必要 community 條件。 |
| `spec.bgp.importPolicy.forbiddenCommunities` | *選填* | array | 選用禁止 community 清單；任一符合就先於接受規則拒絕路徑。空清單不增加禁止條件。 |
| `spec.bgp.importPolicy.nextHopRewrite` | *選填* | string; enum=["", "peer-address", "unchanged"] | 預設 peer-address；unchanged 保留廣告的下一跳。在接受路徑的選擇及 FIB 投影之前套用。對等端只設定此欄位也會建立自己的匯入原則，不繼承路由器允許清單。 |
| `spec.bgp.importPolicy.localPreference` | *選填* | integer | 選用 uint32 LOCAL_PREF，在最佳路徑選擇之前設定於接受的路徑。預設 0 保留 BGP 實作預設值。 |
| `spec.bgp.exportPolicy` | *選填* | object | 選用匯出前綴篩選。對等端前綴會與路由器靜態及動態廣告合併；RR 用戶端另加入路由器匯入前綴，不是取代並限制所有路由器衍生匯出前綴。 |
| `spec.bgp.exportPolicy.allowedPrefixes` | *選填* | array | 選用 CIDR 清單，預設為空。合併後非空清單允許列出前綴及更細路由（至 /32 或 /128），拒絕不符路由。有效清單為空時不安裝匯出篩選。 |
| `spec.bgp.timers` | *選填* | object | 產生對等端計時器，優先序為本設定 timers.profile > timersPreset > BGPRouter.timers。 |
| `spec.bgp.timers.profile` | *選填* | string; enum=["", "default", "fast", "slow"] | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 為 3/9/1 秒，slow 為 60/180/30 秒。不支援直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.bgp.bfd.interface` | *選填* | string | 選用單跳介面。建議 Interface/name，控制器會解析宣告的 Interface.ifname；單獨名稱雖驗證為邏輯介面，卻會原樣傳給 FRR。省略使用多跳。 |
| `spec.bgp.bfd.profile` | *選填* | string; enum=["", "fast", "normal", "slow"] | fast 或省略：minRx=minTx=300ms、倍數 3；normal：500ms/500ms/3；slow：1000ms/1000ms/5。明確個別設定優先。 |
| `spec.bgp.bfd.minRx` | *選填* | string | 選用 Go duration 50ms–60000ms，覆寫設定的接收間隔並轉為整數毫秒。省略使用 profile。這是 BFD 協定計時器，不是路由器重新啟動期限。 |
| `spec.bgp.bfd.minTx` | *選填* | string | 選用 Go duration 50ms–60000ms，覆寫 profile 傳送間隔，輸出為整數毫秒。 |
| `spec.bgp.bfd.detectMultiplier` | *選填* | integer; minimum=1; maximum=50 | 選用整數 1–50；解碼為 0 或省略時使用 profile 預設值（3，slow 為 5）。為協商偵測間隔倍數，不是 routerd 調整嘗試次數。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/transport_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport_support.go) · [pkg/mobilityconfig/sam_transport_slots.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/sam_transport_slots.go) · [pkg/mobilityconfig/transport_mesh.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/transport_mesh.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `reason`, `peersFrom`, `pendingSources`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
