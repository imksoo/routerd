# BGPRouter

[各資源的設定](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

設定本機 GoBGP 執行個體、接納路由及本機起源廣告。核心安裝與遠端可達性不同於 BGP Established 狀態。

## 指定條件與參照關係

- 實際操作目前只支援一個 BGPRouter 且不支援 VRF。驗證接受不同 ASN/VRF/監聽執行個體不代表執行階段支援。
- 路由器匯入、connected 與 static 再分配三組前綴不可跨組重疊；清單內遮罩後重複也會拒絕。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 選填; string; enum=["", "gobgp"]
    communities: # 選填; object
      accept: # 選填; array
        - "<string>" # string
      send: "" # 選填; string; enum=["", "standard", "extended", "both"]
      set: # 選填; object
        in: # 選填; array
          - "<string>" # string
        out: # 選填; array
          - "<string>" # string
    convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
    gracefulRestart: # 選填; object
      enabled: true # 選填; boolean
      restartTime: "<string>" # 選填; string
      stalePathTime: "<string>" # 選填; string
    importPolicy: # 選填; object
      allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixes: # 選填; array
        - "<string>" # string
      forbiddenCommunities: # 選填; array
        - "<string>" # string
      localPreference: 0 # 選填; integer
      nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 選填; array
        - "<string>" # string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    redistribute: # 選填; object
      connected: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
      static: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 選填; string
    watcher: # 選填; object
      maxPrefixes: 1 # 選填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 選填; string
      pollInterval: "<string>" # 選填; string
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
| `spec.importPolicy` | 無覆寫對等端繼承的選用路由器匯入原則。空設定不安裝路由器匯入篩選，但不會自動授權所有收到路由安裝至核心 FIB。 |
| `spec.importPolicy.allowedPrefixes` | 選用 IPv4/IPv6 CIDR 允許清單，依前綴及允許的更細前綴長度比對。拒絕遮罩後重複的前綴。為空時，不在其他已設定匯入條件上加入前綴條件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 整數 0–128。省略或 0 使用每個允許前綴自身長度。正值須大於等於各前綴長度、小於等於家族上限及正數最大長度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 整數 0–128。省略或 0 時 IPv4 為 32、IPv6 為 128。正值須涵蓋所有列出前綴長度且不超過家族上限。動態 leaf 接納要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | 選用 community 清單；接受的路徑必須包含全部列出值。空清單不增加必要 community 條件。 |
| `spec.importPolicy.forbiddenCommunities` | 選用禁止 community 清單；任一符合就先於接受規則拒絕路徑。空清單不增加禁止條件。 |
| `spec.importPolicy.nextHopRewrite` | 預設 peer-address；unchanged 保留廣告的下一跳。在接受路徑的選擇及 FIB 投影之前套用。對等端只設定此欄位也會建立自己的匯入原則，不繼承路由器允許清單。 |
| `spec.importPolicy.localPreference` | 選用 uint32 LOCAL_PREF，在最佳路徑選擇之前設定於接受的路徑。預設 0 保留 BGP 實作預設值。 |
| `spec.exportPolicy` | 選用匯出前綴篩選。對等端前綴會與路由器靜態及動態廣告合併；RR 用戶端另加入路由器匯入前綴，不是取代並限制所有路由器衍生匯出前綴。 |
| `spec.exportPolicy.allowedPrefixes` | 明確的本機起源廣告 CIDR，預設為空，與 redistribute.connected/static 及動態廣告合併，也用於對等端匯出篩選。不檢查 OS 路由存在就會起源廣告。 |
| `spec.timers` | 選用計時器設定。目前控制器不將路由器層級 timers 繼承給對等端，需要時請在各對等端明確設定。 |
| `spec.timers.profile` | 接受 default/fast/slow，但目前執行階段不以路由器層級值建立對等端計時器，請設定 BGPPeer/BGPDynamicPeer 的 timers.profile。 |
| `spec.when` | 選用的共用啟用條件，省略表示無條件。state/all/any、參照驗證與結構描述限制請見共用欄位。 |
| `spec.asn` | 必填非零 uint32 本機 ASN。設定拒絕 BGPRouter 間重複使用；目前執行階段僅支援一個 BGPRouter。 |
| `spec.routerID` | 必填 IPv4 格式 BGP 路由器識別碼，與傳輸位址不同。未指定介面的 BFD 也將其用作本機多跳位址。 |
| `spec.vrf` | 選用 VRF/name 或一般 VRF 名稱，須參照已宣告 VRF。驗證接受，但實際 GoBGP 控制器拒絕任何非空 vrf，回報 GoBGPVRFUnsupported。實際使用請省略。 |
| `spec.listen` | 選用本機 TCP 監聽設定，預設所有位址、埠 179，不是選擇遠端 TCP 埠。 |
| `spec.listen.address` | 選用本機 IPv4/IPv6 綁定位址，空值讓 GoBGP 監聽所有位址。必須為 IP，不接受資源參照或主機名稱。 |
| `spec.listen.port` | TCP 監聽埠 1–65535，省略或 0 為 179。拒絕設定中的位址/埠衝突。 |
| `spec.redistribute` | 選用的明確廣告前綴群組。目前執行階段將 connected/static 與 exportPolicy.allowedPrefixes 聯集，不從 OS 探索相符路由。 |
| `spec.redistribute.connected` | 選用的 connected 標示廣告群組，預設為空；不以核心直連路由存在與否作為廣告條件。 |
| `spec.redistribute.connected.allowedPrefixes` | 明確起源的 IPv4/IPv6 CIDR，預設為空，不可與路由器匯入或 static 再分配前綴重疊。不要求對應直連路由存在。 |
| `spec.redistribute.static` | 選用的 static 標示廣告群組，預設為空，不會自動列舉 IPv4StaticRoute/IPv6StaticRoute 資源。 |
| `spec.redistribute.static.allowedPrefixes` | 明確起源的 IPv4/IPv6 CIDR，預設為空，不可與路由器匯入或 connected 前綴重疊，不檢查核心靜態路由存在。 |
| `spec.communities` | 選用 community 設定。目前僅路由器 set.out 影響本機起源廣告；send、accept、set.in 會解碼及驗證但不使用。 |
| `spec.communities.send` | 接受 standard/extended/both 或空，但不影響操作；GoBGP 對等端固定使用 SendCommunity=3。 |
| `spec.communities.accept` | 選用且受驗證的標準 community 清單，預設為空，目前不篩選匯入。實際比對請用 importPolicy.requiredCommunities/forbiddenCommunities。 |
| `spec.communities.set` | 選用的傳入/傳出 community 清單，目前控制器僅使用路由器 out。 |
| `spec.communities.set.in` | 選用且受驗證的清單，預設為空，目前不套用至匯入路徑。 |
| `spec.communities.set.out` | 附加於 routerd 本機起源廣告的標準 community，預設為空，不改寫所有轉送路徑。使用標準 16 位元的組成的 asn:value 或支援的已知名稱，此處不編碼 extended community。 |
| `spec.convergenceProfile` | default、省略或 stable 預設啟用 graceful restart；fast 預設停用，可由 gracefulRestart.enabled 明確覆寫。不會自動選擇 timers.profile。 |
| `spec.gracefulRestart` | 選用 graceful restart 設定，選擇收斂設定後也用於對等端。明確 enabled 優先於設定預設值。 |
| `spec.gracefulRestart.enabled` | 選用布林值，fast 收斂以外預設 true。false 即使指定時間也會停用 GR。 |
| `spec.gracefulRestart.restartTime` | 正數 Go duration，預設 120 秒，轉為整秒時截斷小數，低於一秒可能變成 0，請使用整秒。 |
| `spec.gracefulRestart.stalePathTime` | 保留過時路由的正數 Go duration，預設 360 秒，轉為整秒。GR 停用時不使用。 |
| `spec.watcher` | 選用狀態觀察設定，不是 BGP keepalive/hold 計時器或對等端接納限制。 |
| `spec.watcher.pollInterval` | Go duration，至少 3 秒。有效輪詢從 15 秒起僅採用較短值，因此大於 15 秒不會延長間隔。 |
| `spec.watcher.maxPrefixes` | 觀察前綴上限，預設 4096，明確正值須小於 1000000。不設定遠端工作階段的 maximum-prefix 關閉原則。 |
| `spec.watcher.peerStateChangeThrottle` | 限制重複對等端狀態事件發布的非負 Go duration，省略或 0 不節流，不延遲 BGP 協定狀態機。 |
| `spec.backend` | 預設 gobgp，也是唯一接受的後端，使用 routerd-bgp 精靈及控制用戶端，不是 FRR BGP 設定檔。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go) · [pkg/bgp/watcher.go](https://github.com/imksoo/routerd/blob/main/pkg/bgp/watcher.go) · [pkg/controller/bgp/fib_linux.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_linux.go) · [pkg/controller/bgp/fib_bsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/fib_bsd.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `peers`, `prefixes`, `establishedPeers`, `acceptedPrefixes`, `observedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
