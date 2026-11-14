# SAMEnrollmentClaim

[リソース別の設定](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

SAM登録へ送るleaf自身の識別・トランスポート・所有権の要求を宣言します。期限・失効はRR・コントローラーの管理状態で、leafのjoin HMACには含めません。

## 指定条件と参照関係

- ローカルで扱う要求は既存SAMEnrollmentPolicyが必要です。SAMEnrollmentClientの要求はローカルにないリモートポリシーを指定でき、RRで登録・HMAC・認可を検証します。ローカルの形の検証成功は登録成功ではありません。
- joinTokenFrom利用時はnonce・timestamp・HMACが必須で、設定されたaudienceと一致し、ポリシー内のnonce重複は禁止です。HMACはdirectMesh:trueを含む正規化leafペイロードのSHA-256で、署名対象の識別・要求を変えると再署名が必要です。
- 失効・期限切れ要求は派生トポロジーから除きます。所有アドレスがないdirect要求はトランスポート接続を保持できても移動経路の受信を許可しません。ポリシー全体の範囲をdirectの全/32許可には使いません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 任意; object
      asn: 1 # 任意; integer; minimum=1
      routerID: "<string>" # 任意; string
    directMesh: true # 任意; boolean
    endpoint: "<string>" # 任意; string
    expiresAt: "<string>" # 任意; string
    joinAudience: "<string>" # 任意; string
    joinHMAC: "<string>" # 任意; string
    joinNonce: "<string>" # 任意; string
    joinTimestamp: "<string>" # 任意; string
    leafID: "<string>" # 必須; string
    mobility: # 任意; object
      ownedAddresses: # 任意; array
        - "<string>" # string
    policyRef: "<string>" # 必須; string
    revoked: true # 任意; boolean
    rrSetRef: "<string>" # 任意; string
    tunnelAddress: "<string>" # 必須; string
    wireGuard: # 任意; object
      allowedIPs: # 任意; array
        - "<string>" # string
      endpoint: "<string>" # 任意; string
      persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.policyRef` | 必須のSAMEnrollmentPolicy/nameです。SAMEnrollmentClientがリモート登録に使うleaf要求以外は、ローカルの存在が必要です。 |
| `spec.rrSetRef` | 形の上では任意ですが、SAMEnrollmentClient実行時にはSAMRRSet/nameが必須です。ポリシーのrrSetRefがあれば一致させます。実行時RRトポロジーの名前で、静的記述するリソースではありません。 |
| `spec.leafID` | 必須の安定したノード識別子です。allowedLeafIDs.patternに照合し、ポリシー内で一意にします。生成nodeRefにも使います。 |
| `spec.joinAudience` | 任意の署名対象audienceです。join-token認証が有効でポリシーaudienceが非空なら一致が必要です。 |
| `spec.joinNonce` | 任意の署名対象nonceです。joinHMACまたはjoin-tokenポリシー時に必須で、同ポリシー内の重複を拒否します。このYAML指定では自動生成しません。 |
| `spec.joinTimestamp` | 任意の署名対象RFC3339・RFC3339Nano時刻です。join-tokenポリシー時と、明示expiresAtをポリシーTTLに照合する場合は必須です。指定TTLはこの時刻があればここからの寿命も制限します。 |
| `spec.joinHMAC` | 正規化要求の任意の16進HMAC-SHA256署名で、join-tokenポリシーでは必須です。共通鍵そのものやControlAPI Bearerトークンではありません。 |
| `spec.tunnelAddress` | 必須のIPv4アドレス・/32 CIDRです。policy.tunnelAddressPrefixesの許可範囲内かつポリシー内で一意にし、自動割当ではありません。 |
| `spec.endpoint` | 任意の外側トンネルIP終端です。policy.endpointPrefixesが非空ならそのIPv4許可範囲内が必要です。WireGuard UDP終端とは別です。 |
| `spec.wireGuard` | 任意のWireGuard公開情報です。1項目でも非空ならpublicKeyが必要で、秘密鍵は含めません。 |
| `spec.wireGuard.publicKey` | WireGuard情報があれば必須の公開鍵です。非空鍵はポリシー内で一意にし、ここで自動生成はしません。 |
| `spec.wireGuard.endpoint` | 任意のWireGuard UDP host:port（1～65535）です。prefix認可では終端IPの解析が必要で、wireGuard.endpointPrefixesまたはpolicy.endpointPrefixesを使います。ホスト名で認可に通るとは扱わないでください。 |
| `spec.wireGuard.allowedIPs` | 任意の追加IPv4 CIDR一覧で、各項目はpolicy.wireGuard.allowedExtraIPPrefixes内が必要です。空なら追加なしで、生成ピアは別にtunnelAddress /32を含めます。 |
| `spec.wireGuard.persistentKeepalive` | 0～65535の整数秒です。既定0は生成トポロジーでポリシー側のkeepaliveを使い、非0値は上書きします。 |
| `spec.directMesh` | 既定falseはRRピアのみです。trueはポリシーのdirectピアグループ参加を要求する署名対象で、ポリシー有効化・所有/32認可を回避しません。 |
| `spec.mobility` | 任意の宣言した移動所有権です。既定は所有アドレスなしで、端末探索そのものは行いません。 |
| `spec.mobility.ownedAddresses` | ポリシーのmobilityPrefixes・MobilityPool参照で許可されたIPv4アドレス・/32です。同ポリシーの有効要求間重複は拒否し、direct BGP受信は許可された所有/32だけに絞ります。 |
| `spec.bgp` | 任意の署名対象BGP識別要求で、BGPRouterリソース参照ではありません。 |
| `spec.bgp.asn` | 署名要求に含める任意のuint32 ASNで、デコード既定0です。スキーマは明示時1以上ですが、形の検証は非0を必須としません。ポリシートランスポートpeerASNの上書きではありません。 |
| `spec.bgp.routerID` | 任意のIPv4 BGP識別子で、自動既定値はありません。非空IDはポリシー内で一意にし、BGPRouterを作成しません。 |
| `spec.expiresAt` | 任意のRR・管理者側RFC3339期限で、join HMAC対象外です。通常登録でポリシーTTLまたは既定5分の期限を設定します。両方指定時はjoinTimestamp+ポリシーTTLを超えられず、期限切れ要求は除外します。 |
| `spec.revoked` | 既定falseです。trueはRR・管理者側の失効状態で、join HMAC対象外となりトポロジー取り込みを防ぎます。leafは署名の再送で保持済み失効を解除できません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `leafID`, `rrSetRef`, `tunnelAddress`, `endpoint`, `revoked`, `expiresAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
