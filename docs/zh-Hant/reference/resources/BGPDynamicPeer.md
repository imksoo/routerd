# BGPDynamicPeer

[各資源的設定](../resources.md#bgpdynamicpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

定義 GoBGP 傳入動態鄰居接納群組，不是個別對等端或 WireGuard/SAM 授權身分。

## 指定條件與參照關係

- 對等端匯入原則被視為已設定時，完整取代路由器原則，不逐欄合併。匯出前綴則與路由器及執行時衍生廣告前綴聯集。
- 目前控制器同時啟用 IPv4/IPv6 unicast。Established 工作階段不會自動授權所有路由安裝至 FIB，也不證明資料轉送可達。
- Ready 表示已套用對等端群組及來源前綴接納規則，不表示已有動態鄰居 Established。動態對等端 GR 預設值來自路由器收斂/GR 設定。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    [ ebgpMultihop: <integer> ]
    [ exportPolicy:
        [ allowedPrefixes:
            - <string>
        ]
    ]
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
    listen:
      sourcePrefixes:
        - <string>
    [ password: <string> ]
    [ passwordFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    peerASN: <integer>
    [ routeReflectorClient: <boolean> ]
    [ routeReflectorClusterID: <string> ]
    routerRef: <string>
    [ timers:
        [ profile: ("" | "default" | "fast" | "slow") ]
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
| `spec.importPolicy` | *選填* | object | 動態 leaf 路由接納要求有效匯入具有非空 IPv4 允許清單及 min=max=32。對等端允許清單為空時，驗證完整採用路由器原則；對等端 nextHopRewrite 無自己的允許清單會被拒絕。請在此指定完整原則，避免僅覆寫附加欄位。 |
| `spec.importPolicy.allowedPrefixes` | *選填* | array | 選用 IPv4/IPv6 CIDR 允許清單，依前綴及允許的更細前綴長度比對。拒絕遮罩後重複的前綴。為空時，不在其他已設定匯入條件上加入前綴條件。 |
| `spec.importPolicy.allowedPrefixLengthMin` | *選填* | integer; minimum=0; maximum=128 | 整數 0–128。省略或 0 使用每個允許前綴自身長度。正值須大於等於各前綴長度、小於等於家族上限及正數最大長度。 |
| `spec.importPolicy.allowedPrefixLengthMax` | *選填* | integer; minimum=0; maximum=128 | 整數 0–128。省略或 0 時 IPv4 為 32、IPv6 為 128。正值須涵蓋所有列出前綴長度且不超過家族上限。動態 leaf 接納要求 min=max=32。 |
| `spec.importPolicy.requiredCommunities` | *選填* | array | 選用 community 清單；接受的路徑必須包含全部列出值。空清單不增加必要 community 條件。 |
| `spec.importPolicy.forbiddenCommunities` | *選填* | array | 選用禁止 community 清單；任一符合就先於接受規則拒絕路徑。空清單不增加禁止條件。 |
| `spec.importPolicy.nextHopRewrite` | *選填* | string; enum=["", "peer-address", "unchanged"] | 預設 peer-address；unchanged 保留廣告的下一跳。在接受路徑的選擇及 FIB 投影之前套用。對等端只設定此欄位也會建立自己的匯入原則，不繼承路由器允許清單。 |
| `spec.importPolicy.localPreference` | *選填* | integer | 選用 uint32 LOCAL_PREF，在最佳路徑選擇之前設定於接受的路徑。預設 0 保留 BGP 實作預設值。 |
| `spec.exportPolicy` | *選填* | object | 選用匯出前綴篩選。對等端前綴會與路由器靜態及動態廣告合併；RR 用戶端另加入路由器匯入前綴，不是取代並限制所有路由器衍生匯出前綴。 |
| `spec.exportPolicy.allowedPrefixes` | *選填* | array | 選用 CIDR 清單，預設為空。合併後非空清單允許列出前綴及更細路由（至 /32 或 /128），拒絕不符路由。有效清單為空時不安裝匯出篩選。 |
| `spec.timers` | *選填* | object | 選用計時器設定。目前控制器不將路由器層級 timers 繼承給對等端，需要時請在各對等端明確設定。 |
| `spec.timers.profile` | *選填* | string; enum=["", "default", "fast", "slow"] | default 或省略：keepalive 30 秒、hold 90 秒、connectRetry 10 秒；fast 為 3/9/1 秒，slow 為 60/180/30 秒。不支援直接指定 keepalive/holdTime/connectRetry YAML。 |
| `spec.when` | *選填* | object | 選用的共用啟用條件，省略表示無條件。state/all/any、參照驗證與結構描述限制請見共用欄位。 |
| `spec.routerRef` | **必填** | string | 必填 BGPRouter/name，參照同一 Router 設定中宣告的路由器，不接受單獨名稱。 |
| `spec.peerASN` | **必填** | integer; minimum=1 | 必填非零 uint32 遠端 ASN，等於本機 ASN 時為 iBGP；routeReflectorClient 要求兩者相等。 |
| `spec.password` | *選填* | string | 選用直接指定的 BGP TCP 認證密鑰，預設無，與非空 passwordFrom 來源互斥，不是 SAM 註冊權杖。 |
| `spec.passwordFrom` | *選填* | object | 選用密鑰來源，使用時 file/env 必須且只能指定一項。省略表示無外部密鑰。不是狀態值或 Kind/name 參照，執行時檔案或環境變數缺少會使對等端調整失敗。 |
| `spec.passwordFrom.file` | *選填* | string | 路由器本機原始密碼文字檔，與 env 及直接 password 互斥。移除尾端 CR/LF，其餘空白保留。 |
| `spec.passwordFrom.env` | *選填* | string | 存放密鑰的 routerd 程序環境變數名稱，不是密鑰本身。與 file 及直接 password 互斥，不自動載入環境檔。 |
| `spec.passwordFrom.base64` | *選填* | boolean | 預設 false。true 對選定 file/env 值執行標準 Base64 解碼，再移除尾端 CR/LF。這是編碼，不是加密。 |
| `spec.ebgpMultihop` | *選填* | integer; minimum=0; maximum=255 | 整數 0–255，預設 0。僅大於 1 時啟用 GoBGP eBGP multihop 並設定 TTL，0/1 保持停用。 |
| `spec.routeReflectorClient` | *選填* | boolean | 預設 false。true 標記 iBGP 路由反射器用戶端，要求 peerASN 等於路由器 ASN，並將路由器匯入前綴加入有效匯出前綴。 |
| `spec.routeReflectorClusterID` | *選填* | string | 選用 IPv4 格式叢集 ID，僅 routeReflectorClient=true 時傳入。空值由 GoBGP 決定，單獨設定不啟用反射。 |
| `spec.listen` | **必填** | object | 必填的傳入接納規則，不是探索到的對等端或 SAM 註冊物件。 |
| `spec.listen.sourcePrefixes` | **必填** | array | 必填且不可為空的 GoBGP 動態鄰居來源 IPv4/IPv6 CIDR 清單，拒絕遮罩後重複。限制傳入 TCP 對等端位址，不是可廣告路由；路由接納由 importPolicy 另行處理。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `peerGroup`, `sourcePrefixes`, `sourcePrefixCount`, `discoveredPeers`, `discoveredPeerCount`, `acceptedRouteCount`, `rejectedRouteCount`, `rejectedRouteSummary`, `observedAt`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
