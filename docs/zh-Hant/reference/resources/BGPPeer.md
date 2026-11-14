# BGPPeer

[各資源的設定](../resources.md#bgppeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在已宣告的 BGPRouter 設定一個或多個靜態 BGP 鄰居。

## 指定條件與參照關係

- 對等端匯入原則被視為已設定時，完整取代路由器原則，不逐欄合併。匯出前綴則與路由器及執行時衍生廣告前綴聯集。
- 目前控制器同時啟用 IPv4/IPv6 unicast。Established 工作階段不會自動授權所有路由安裝至 FIB，也不證明資料轉送可達。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 選填; string
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
    ebgpMultihop: 0 # 選填; integer; minimum=0; maximum=255
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
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
    passiveMode: true # 選填; boolean
    password: "<string>" # 選填; string
    passwordFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    peerASN: 1 # 必填; integer; minimum=1
    peers: # 選填; array
      - "<string>" # string
    routeReflectorClient: true # 選填; boolean
    routeReflectorClusterID: "<string>" # 選填; string
    routerRef: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
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
| `spec.importPolicy` | 選用匯入原則。對等端若無非空前綴、community 條件、下一跳改寫或 localPreference，會完整繼承路由器原則；僅前綴長度限制不算覆寫。 |
| `spec.importPolicy.allowedPrefixes` | 選用 IPv4/IPv6 CIDR 允許清單，依前綴及允許的更細前綴長度比對。拒絕遮罩後重複的前綴。為空時，不在其他已設定匯入條件上加入前綴條件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | 整數 0–128。省略或 0 使用每個允許前綴自身長度。正值須大於等於各前綴長度、小於等於家族上限及正數最大長度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | 整數 0–128。省略或 0 時 IPv4 為 32、IPv6 為 128。正值須涵蓋所有列出前綴長度且不超過家族上限。動態 leaf 接納要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | 選用 community 清單；接受的路徑必須包含全部列出值。空清單不增加必要 community 條件。 |
| `spec.importPolicy.forbiddenCommunities` | 選用禁止 community 清單；任一符合就先於接受規則拒絕路徑。空清單不增加禁止條件。 |
| `spec.importPolicy.nextHopRewrite` | 預設 peer-address；unchanged 保留廣告的下一跳。在接受路徑的選擇及 FIB 投影之前套用。對等端只設定此欄位也會建立自己的匯入原則，不繼承路由器允許清單。 |
| `spec.importPolicy.localPreference` | 選用 uint32 LOCAL_PREF，在最佳路徑選擇之前設定於接受的路徑。預設 0 保留 BGP 實作預設值。 |
| `spec.exportPolicy` | 選用匯出前綴篩選。對等端前綴會與路由器靜態及動態廣告合併；RR 用戶端另加入路由器匯入前綴，不是取代並限制所有路由器衍生匯出前綴。 |
| `spec.exportPolicy.allowedPrefixes` | 選用 CIDR 清單，預設為空。合併後非空清單允許列出前綴及更細路由（至 /32 或 /128），拒絕不符路由。有效清單為空時不安裝匯出篩選。 |
| `spec.timers` | 選用計時器設定。目前控制器不將路由器層級 timers 繼承給對等端，需要時請在各對等端明確設定。 |
| `spec.timers.profile` | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 為 3/9/1 秒，slow 為 60/180/30 秒。不支援直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.when` | 選用的共用啟用條件，省略表示無條件。state/all/any、參照驗證與結構描述限制請見共用欄位。 |
| `spec.routerRef` | 必填 BGPRouter/name，參照同一 Router 設定中宣告的路由器，不接受單獨名稱。 |
| `spec.peerASN` | 必填非零 uint32 遠端 ASN，等於本機 ASN 時為 iBGP；routeReflectorClient 要求兩者相等。 |
| `spec.peers` | 必填且不可為空的位址/主機名稱清單，去除首尾空白後不可重複或包含空白。設定驗證不解析主機名稱，直接交給 GoBGP，套用時才確認接受性。不同 BGPPeer 的重複位址未在此拒絕，執行階段共用同一位址鍵。 |
| `spec.passiveMode` | 預設 false 正常主動連線，true 設定 GoBGP passive transport 等待傳入連線，不是動態來源前綴接納規則。 |
| `spec.password` | 選用直接指定的 BGP TCP 認證密鑰，預設無，與非空 passwordFrom 來源互斥，不是 SAM 註冊權杖。 |
| `spec.passwordFrom` | 選用密鑰來源，使用時 file/env 必須且只能指定一項。省略表示無外部密鑰。不是狀態值或 Kind/name 參照，執行時檔案或環境變數缺少會使對等端調整失敗。 |
| `spec.passwordFrom.file` | 路由器本機原始密碼文字檔，與 env 及直接 password 互斥。移除尾端 CR/LF，其餘空白保留。 |
| `spec.passwordFrom.env` | 存放密鑰的 routerd 程序環境變數名稱，不是密鑰本身。與 file 及直接 password 互斥，不自動載入環境檔。 |
| `spec.passwordFrom.base64` | 預設 false。true 對選定 file/env 值執行標準 Base64 解碼，再移除尾端 CR/LF。這是編碼，不是加密。 |
| `spec.ebgpMultihop` | 整數 0–255，預設 0。僅大於 1 時啟用 GoBGP eBGP multihop 並設定 TTL，0/1 保持停用。 |
| `spec.routeReflectorClient` | 預設 false。true 標記 iBGP 路由反射器用戶端，要求 peerASN 等於路由器 ASN，並將路由器匯入前綴加入有效匯出前綴。 |
| `spec.routeReflectorClusterID` | 選用 IPv4 格式叢集 ID，僅 routeReflectorClient=true 時傳入。空值由 GoBGP 決定，單獨設定不啟用反射。 |
| `spec.convergenceProfile` | 選用 default/fast/stable，省略繼承 BGPRouter convergenceProfile。選擇 graceful restart 預設值，不是 timers.profile；路由器 gracefulRestart.enabled 仍優先覆寫。 |
| `spec.bfd` | 選用的已宣告 BFD/name 參照。其 peer 必須識別此 BGPPeer 或其中一個對等端位址；Up/Established 後 BFD Down 會觸發 BGP 重設。省略不連結 BFD。 |
| `spec.communities` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |
| `spec.communities.send` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |
| `spec.communities.accept` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |
| `spec.communities.set` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |
| `spec.communities.set.in` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |
| `spec.communities.set.out` | 目前 BGPPeer 僅解碼及驗證，不在執行時使用。GoBGP SendCommunity 固定 both，實際匯入比對請設定 importPolicy。對等端 community 設定不繼承或變更路由器本機廣告 community。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
