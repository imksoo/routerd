# SAMEnrollmentClaim

[各資源的設定](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

宣告提交 SAM 註冊的 leaf 自有身分、傳輸與移動意圖。到期及撤銷是 RR/控制器管理狀態，不屬於 leaf join HMAC。

## 指定條件與參照關係

- 本機服務的要求需既有 SAMEnrollmentPolicy。由 SAMEnrollmentClient 參照的要求可指定本機不存在的遠端原則，由 RR 在提交時執行接納/HMAC/授權檢查。本機結構驗證通過不等於接納成功。
- 設定 joinTokenFrom 時需 nonce/timestamp/HMAC，audience 須符合設定值，且同一原則內 nonce 不可衝突。HMAC 為正規 leaf 酬載的 SHA-256，選用 direct 時包含 directMesh:true；變更簽署身分/意圖須重新簽署。
- 已撤銷或過期要求排除於衍生拓撲。無擁有位址的 direct 要求仍可保留傳輸鄰接，但不允許匯入移動路由；原則整體範圍不代表 無限制 direct /32 授權。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 選填; object
      asn: 1 # 選填; integer; minimum=1
      routerID: "<string>" # 選填; string
    directMesh: true # 選填; boolean
    endpoint: "<string>" # 選填; string
    expiresAt: "<string>" # 選填; string
    joinAudience: "<string>" # 選填; string
    joinHMAC: "<string>" # 選填; string
    joinNonce: "<string>" # 選填; string
    joinTimestamp: "<string>" # 選填; string
    leafID: "<string>" # 必填; string
    mobility: # 選填; object
      ownedAddresses: # 選填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 選填; boolean
    rrSetRef: "<string>" # 選填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 選填; object
      allowedIPs: # 選填; array
        - "<string>" # string
      endpoint: "<string>" # 選填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.policyRef` | 必填 SAMEnrollmentPolicy/name，除 SAMEnrollmentClient 用於遠端接納的 leaf 要求外，必須在本機存在。 |
| `spec.rrSetRef` | 結構上選用，但 SAMEnrollmentClient 執行時需 SAMRRSet/name。原則設定 rrSetRef 時須相等。此為執行時 RR 拓撲名稱，不是需編寫的靜態資源。 |
| `spec.leafID` | 必填穩定節點身分，須符合 allowedLeafIDs.pattern 且在原則內唯一，用作產生的 nodeRef。 |
| `spec.joinAudience` | 選用且納入簽署的 audience；啟用 join-token 認證且原則 audience 非空時須相等。 |
| `spec.joinNonce` | 選用且納入簽署的 nonce，設定 joinHMAC 或使用 join-token 原則時必填。同一原則內重複會拒絕，此 YAML 欄位不自動產生。 |
| `spec.joinTimestamp` | 選用且納入簽署的 RFC3339/RFC3339Nano 時間戳。join-token 原則或以原則 TTL 檢查明確 expiresAt 時必填；有此時間時，TTL 也限制從此起的要求壽命。 |
| `spec.joinHMAC` | 正規要求的選用十六進位 HMAC-SHA256 簽章，join-token 原則時必填。是簽章，不是 join 密鑰或 ControlAPI Bearer 權杖。 |
| `spec.tunnelAddress` | 必填純 IPv4 位址或 /32 CIDR，須在 policy.tunnelAddressPrefixes 授權範圍且在原則內唯一，不自動分配。 |
| `spec.endpoint` | 選用外層隧道 IP 端點，policy.endpointPrefixes 非空時須位於該 IPv4 允許清單，與 WireGuard UDP 端點不同。 |
| `spec.wireGuard` | 選用公開 WireGuard 資料，任一子欄位非空時需 publicKey，不包含私密金鑰。 |
| `spec.wireGuard.publicKey` | 提供 WireGuard 資料時必填公開金鑰，非空金鑰在原則內須唯一，此處不自動產生。 |
| `spec.wireGuard.endpoint` | 選用 WireGuard UDP host:port（1–65535）。前綴接納要求端點 IP 可解析，使用 wireGuard.endpointPrefixes 或 policy.endpointPrefixes；不可假設主機名稱會通過接納。 |
| `spec.wireGuard.allowedIPs` | 選用額外 IPv4 CIDR，各項須包含於 policy.wireGuard.allowedExtraIPPrefixes。空值無額外前綴，產生對等端另包含 tunnelAddress /32。 |
| `spec.wireGuard.persistentKeepalive` | 整數秒 0–65535，預設 0 在產生拓撲時使用原則 WireGuard keepalive，非零覆寫預設值。 |
| `spec.directMesh` | 預設 false 僅使用 RR 對等端。true 要求加入原則 direct 對等群組並納入簽署，不略過原則啟用或擁有 /32 接納檢查。 |
| `spec.mobility` | 選用宣告移動所有權，預設無擁有位址，本身不探索用戶端。 |
| `spec.mobility.ownedAddresses` | 由原則 mobilityPrefixes/MobilityPool 參照授權的純 IPv4 位址或 /32。同一原則的有效要求間不可重複；direct BGP 匯入僅限接納的擁有 /32。 |
| `spec.bgp` | 選用且納入簽署的 BGP 身分意圖，不是 BGPRouter 資源參照。 |
| `spec.bgp.asn` | 簽署要求中的選用 uint32 ASN，解碼預設 0。結構描述要求提供時 >=1，但結構驗證不另要求非零。不是覆寫原則傳輸 peerASN。 |
| `spec.bgp.routerID` | 選用 IPv4 BGP 識別碼，無衍生預設值。非空 ID 在原則內須唯一，不建立 BGPRouter。 |
| `spec.expiresAt` | 選用 RR/管理者擁有的 RFC3339 到期時間，不納入 join HMAC。接納通常指派原則 TTL 或預設 5 分鐘到期。兩者提供時明確期限不可超過 joinTimestamp+原則 TTL，過期則排除。 |
| `spec.revoked` | 預設 false，true 為 RR/管理者撤銷狀態，不納入 join HMAC，阻止拓撲接納。leaf 無法重送簽章以清除已保存撤銷。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
