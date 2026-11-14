# SAMEnrollmentClaim

[リソース別の設定](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
