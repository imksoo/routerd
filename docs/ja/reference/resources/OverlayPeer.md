# OverlayPeer

[リソース別の設定](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必須; string
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    remote: # 任意; object
      address: "<string>" # 任意; string
      nodeID: "<string>" # 任意; string
    role: "onprem" # 必須; string; enum=["onprem", "cloud"]
    underlay: # 必須; object
      address: "<string>" # 任意; string
      interface: "<string>" # 任意; string
      type: "wireguard" # 必須; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
