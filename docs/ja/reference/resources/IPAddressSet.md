# IPAddressSet

[リソース別の設定](../resources.md#ipaddressset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

固定IPとDNS解決したIPを、対応するnftablesのNAT・経路・ファイアウォールルールで共有します。経路やDNSサービスを作成する機能ではありません。

## 指定条件と参照関係

- addresses・namesのどちらか1件以上が必須で、両方を併用できます。addressesはIPv4・IPv6の単一アドレスで、CIDRではありません。設定リスト内の重複は拒否し、解決後の値は統合・重複排除します。
- 利用側は名前またはIPAddressSet/nameで参照します。nftablesの対応先はLocalServiceRedirect、NAT44Rule・FlowDNATPinhole、EgressRoutePolicy、FirewallRule・FlowPinholeです。未参照の宣言はObserved・referenced:falseを記録するだけで、名前解決やカーネル集合の作成は行いません。
- 更新前に利用側の生成処理がnftables集合を作成する必要があり、存在しなければPending/SetMissingです。名前解決は/etc/resolv.confのサーバーへA・AAAAを問い合わせ、DNSUpstream参照は使いません。一部の解決に失敗しても残るアドレスは適用できます。全件解決失敗ではPending/ResolveFailedとなり、その回は既存集合を置換しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # 任意; array
      - "<string>" # string
    names: # 任意; array
      - "<string>" # string
    refreshInterval: "<string>" # 任意; string
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
| `spec.addresses` | 任意の固定IPv4・IPv6一覧で、既定は空です。namesも空なら必須です。IPv4射影IPv6はIPv4へ正規化し、サブネット展開はしません。 |
| `spec.names` | 任意のDNS名一覧で、既定は空です。addressesも空なら必須です。空文字と前後空白を除いた重複を拒否します。A・AAAA結果を固定値に加え、ワイルドカード名の展開は行いません。 |
| `spec.refreshInterval` | 任意のGo durationです。既定は最小の正のDNS TTLの半分で、下限は1mです。正のTTLがなければ5mを仮定し2m30sです。正の指定値は更新を早める方向にだけ作用し、1m未満も指定できます。0・負値は解析できますが短縮効果はありません。実際の更新はコントローラーの再実行時です。 |
| `spec.when` | 任意の共通有効化条件で、省略時は無条件です。state・all・anyと利用側の有効化条件は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `updatedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
