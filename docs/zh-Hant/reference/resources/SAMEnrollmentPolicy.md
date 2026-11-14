# SAMEnrollmentPolicy

[各資源的設定](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

定義 RR 端 SAM leaf 身分、傳輸位址、選用 join 認證及允許移動所有權的接納邊界。leaf 使用接納拓撲，不複製此原則。

## 指定條件與參照關係

- RR 輸出名稱每個原則須唯一，rrSetRef/rrNodeSetRef 成對設定。directMesh 另要求 pair-stable 傳輸定址及 RR 備援。
- 同一原則內檢查 leaf ID、隧道位址、非空 WireGuard 金鑰、擁有位址及非空 BGP router ID 衝突。端點、WireGuard 額外前綴及移動所有權分別授權。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 選填; object
      pattern: "<string>" # 選填; string
    directMesh: # 選填; object
      peerGroupRef: "<string>" # 選填; string
    endpointPrefixes: # 選填; array
      - "<string>" # string
    joinAudience: "<string>" # 選填; string
    joinTokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    mobilityPoolRefs: # 選填; array
      - "<string>" # string
    mobilityPrefixes: # 選填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 選填; string
    rrSetRef: "<string>" # 選填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 選填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 選填; object
      allowedExtraIPPrefixes: # 選填; array
        - "<string>" # string
      endpointPrefixes: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.transportProfileRef` | 必填同一 Router 中既有 SAMTransportProfile/name，將接納的 leaf 綁定至該傳輸設定，不是狀態來源。 |
| `spec.rrSetRef` | 選用執行時輸出名稱 SAMRRSet/name，須與 rrNodeSetRef 同時設定且跨註冊原則唯一。這是產生快照名稱，不是靜態編寫 SAMRRSet。direct mesh 與 leaf 用戶端擷取需要此值。 |
| `spec.rrNodeSetRef` | 供應 RR 身分的既有 SAMNodeSet/name，與 rrSetRef 同時必填。該 NodeSet 所有節點須為 routeReflector:true，不在原則內複製 RR 登錄。 |
| `spec.joinTokenFrom` | 選用共用 HMAC 密鑰來源。省略不要求 join HMAC，由受保護 ControlAPI 作為信任邊界。提供時 file/env 只能一項，要求須有 nonce/timestamp/HMAC。與 ControlAPI Bearer 認證不同。 |
| `spec.joinTokenFrom.file` | 路由器本機原始 HMAC 密鑰檔，與 env 互斥，移除首尾空白。即使離線驗證無法讀取，實際提交時缺少密鑰仍會失敗。 |
| `spec.joinTokenFrom.env` | 存放 HMAC 密鑰的 routerd 程序環境變數名稱，與 file 互斥，不自動載入環境檔。 |
| `spec.joinTokenFrom.base64` | 預設 false，true 對去除首尾空白的來源執行標準 Base64 解碼，解碼位元組作為 HMAC 金鑰。Base64 不是加密。 |
| `spec.joinAudience` | 設定 joinTokenFrom 時選用的預期要求 audience，非空值須等於 claim.joinAudience，無預設 audience。 |
| `spec.allowedLeafIDs` | 選用 leaf 身分限制物件，省略不施加正規表示式限制。 |
| `spec.allowedLeafIDs.pattern` | 比對去除首尾空白 leafID 的 Go 正規表示式。空值允許其他條件有效的任意 ID，不自動加首尾錨點，完整比對請用 ^...$。 |
| `spec.tunnelAddressPrefixes` | 必填且不可為空的 IPv4 CIDR 清單，授權 claim.tunnelAddress /32 身分，不自動分配位址。 |
| `spec.endpointPrefixes` | 選用外層端點 IPv4 CIDR 允許清單，空值不限制端點前綴。wireGuard.endpointPrefixes 為空時，也作為 WireGuard 端點允許清單。 |
| `spec.wireGuard` | 選用接納 leaf 的 WireGuard 控制。使用其他操作性 WireGuard 設定時需 interface，僅端點前綴驗證不供應 WireGuard 裝置。 |
| `spec.wireGuard.interface` | 選用既有 WireGuardInterface metadata.name，不是 Kind/name。限制匯入此原則的本機 WireGuard 介面，不自動建立。 |
| `spec.wireGuard.endpointPrefixes` | 選用 IPv4 CIDR，限制要求的 WireGuard 外層端點 IP。空值使用 endpointPrefixes，兩者皆空則無此前綴限制。 |
| `spec.wireGuard.allowedExtraIPPrefixes` | 選用 IPv4 CIDR，授權額外 claim.wireGuard.allowedIPs。隧道 /32 會另行加入產生的對等端；空值不授權額外前綴。 |
| `spec.wireGuard.persistentKeepalive` | 秒數 0–65535，預設 0。要求 keepalive 為 0 時供產生的 WireGuard 對等端使用，要求的明確非零值優先。 |
| `spec.directMesh` | 選用且限於此原則的 direct leaf mesh 輸出，不移除 RR 備援路徑。 |
| `spec.directMesh.peerGroupRef` | SAMPeerGroup/name 執行時輸出，空值停用 direct mesh。需要 rrSetRef 及參照傳輸的 addressingMode: pair-stable，跨原則須唯一，不是靜態編寫 SAMPeerGroup。 |
| `spec.mobilityPoolRefs` | 選用既有 MobilityPool/name 清單，提供允許的所有權前綴，與 mobilityPrefixes 合併，不要求建立集區或雲端資源。 |
| `spec.mobilityPrefixes` | 選用額外 CIDR，授權要求的擁有 IPv4 /32，與參照集區合併。此值及集區參照皆空時不授權擁有位址。 |
| `spec.ttl` | 選用 Go duration，實際提交接納省略時預設租約 5 分鐘。設定 TTL 也限制從 joinTimestamp 起的要求壽命及明確 expiresAt。結構驗證僅解析時間，但執行時無效或非正壽命會視為過期。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `acceptedClaims`, `skippedClaims`, `leafIDs`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
