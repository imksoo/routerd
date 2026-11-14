# RouterdCluster

[リソース別の設定](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

ファイルのリースとOSファイルロックで、どのrouterdが変更処理を行うかを調整します。VRRPの選出や対向ネットワークの自動探索ではありません。

## 指定条件と参照関係

- 参加者間で同じleasePathと信頼できるロック動作が必要です。peersは識別子で、接続先ではありません。リース実装は空でない一覧を調整の有効化に使い、対向への通信や参加資格の照合は行いません。待機側の変更controllerはdry-runで動作します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 任意; string
    leasePath: "<string>" # 任意; string
    leaseTTL: "<string>" # 任意; string
    peers: # 必須; array
      - "<string>" # string
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.peers` | 空白・重複を除去した後に2件以上となる一覧が必須です。既定値はなく、Kind/名前の参照や、実際に接続するネットワークアドレスではありません。 |
| `spec.leaseTTL` | リースの有効期間。省略時30sで、明示する場合は5s〜10mのGo durationです。排他的な変更はロックが制御し、期限だけでネットワーク障害を検知する仕組みではありません。 |
| `spec.leasePath` | 改行を含まない絶対パスのリースファイル。必須で既定値はありません。ホストごとに別のファイルを用意してもホスト間の調整にはなりません。 |
| `spec.identity` | ローカルのリース保持者の識別子。省略時は空白を除去したos.Hostname()です。Router.metadata.nameからは決めません。 |
| `spec.when` | 有効化条件。省略時は条件なし。共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
