# 共通の参照と条件指定

## リソースの識別と値の参照

`metadata.name`は種類ごとのリソース名です。値の取得元を表す`resource`は`Kind/名前`で指定し、OSのインターフェース名やURLとは区別します。通常の状態参照では、参照先が同じRouterに宣言され、その種類の提供フィールドに指定項目があることを検証します。`optional: true`でも、この確認は省略しません。

```yaml
dnsServerFrom:
  - resource: IPv4StaticAddress/lan-address
    field: address
```

| 項目 | 意味・既定値・指定条件 |
| --- | --- |
| `resource` | 取得元の`Kind/名前`。参照を使う場合、fieldを指定する場合に必要です。ワイルドカードは使いません。 |
| `field` | address、phase、zoneなどの取得項目。参照先が提供する項目だけを指定します。下位の読取り処理にはphaseの既定値がありますが、通常の設定検証ではresourceを指定した参照にfieldの明示が必要です。省略せず指定してください。 |
| `optional` | 省略時false。対応する利用側で、稼働中の値が未解決でも処理を続ける指定です。設定上存在しないリソースの参照を許可するものではありません。値の欠落時の扱いは各リソースの説明を参照してください。 |

`ValuesFromStoreOrRouter`を使う取得処理では、DNSZone.zoneとIPv4StaticAddress.addressを宣言済み設定から先に読み、その後に状態を読みます。他の取得処理は状態を直接読みます。参照を宣言しただけでは、動的な値の準備完了や実行順序は保証されません。

## 依存条件

`dependsOn`には条件を配列で指定し、全要素の成立を要求します。省略や空配列では依存条件を設けません。

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
```

| 項目 | 意味・既定値・指定条件 |
| --- | --- |
| `resource` | 依存先のKind/名前。設定上の参照先と、参照するフィールドの存在が必要です。 |
| `field` | 省略時phase。phaseを指定した場合は使いません。 |
| `phase` | 参照先phaseとの一致を要求する略記。空でなければfieldとequalsより優先します。 |
| `equals` | 返された値のいずれかに要求する完全一致文字列。省略時は先頭の値が空白でないことを要求します。 |
| `notEmpty` | 省略時false。正規化後の先頭値が空白でないことを確認します。現行の読取り処理は既に空白の値を除外するため、通常はこの指定を加えても判定結果は変わりません。 |
| `optional` | 省略時false。実行時に値がない場合はtrueのときだけ成立します。取得できた不一致の値を成立にはせず、設定上の参照先検証も緩和しません。 |

## 有効化条件

`when`を省略すると条件を設けません。空でない条件では、各階層で`state`・`all`・`any`のどれか1つだけを指定します。allとanyには入れ子の条件を配列で記述し、空の子要素は使えません。再帰するallとanyはJSON Schemaには列挙されていませんが、実装では受理します。

```yaml
when:
  all:
    - state:
        DHCPv6PrefixDelegation/wan-pd.phase:
          equals: Bound
    - state:
        HealthCheck/upstream.phase:
          equals: Healthy
```

stateは全項目をANDで評価し、allは子条件のAND、anyはORです。リソースの状態キーには`Kind/名前.field`、`Kind/名前.status.field`、それぞれを`${...}`で囲んだ表記を使えます。リソース形式のキーは宣言済みリソースと提供フィールドを検証し、それ以外の形式は状態ストアのキーとして扱います。

| 状態の比較項目 | 意味と省略時の扱い |
| --- | --- |
| `exists` | 省略時は存在の制約なし。trueは状態set、falseはunsetを要求します。unknownはunsetと区別します。 |
| `equals` | 空でない値を指定すると、状態setと、大文字・小文字を区別した完全一致を要求します。空値では比較しません。不在条件にはexistsやstatusを使います。 |
| `in` | 空でない配列なら、状態setと、いずれかの要素への完全一致を要求します。空配列では制約を設けません。 |
| `contains` | 空でない文字列なら、状態setと、大文字・小文字を区別した部分文字列の一致を要求します。空値では制約を設けません。 |
| `status` | 任意の状態ストアの状態set・unset・unknown。リソースの動作段階を表すphaseとは別です。 |
| `for` | 任意のGo duration。観測した状態の経過時間が指定値以上であることを要求します。条件全体を計時する独立タイマーではありません。構文は検証しますが、ここでは正の期間を必須化しません。 |
| `maxAge` | 任意の正のGo duration。状態の経過時間が指定値以下であることを要求します。forと併用した場合は両方の経過時間条件を満たす必要があります。 |

同じ状態に複数の比較項目を指定すると、すべてを満たす必要があります。未解決のunknown値は単なるfalseではなく未確定として扱われる場合があり、実効設定の一部経路では未確定のリソースを残します。確定済みだけを選ぶ経路とは異なります。VXLANTunnelではunknownを明示的に許可する条件を拒否します。保留・撤去の具体的な動作は各リソースの説明を参照してください。

実装：[参照検証](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go)、[条件検証](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)、[値と依存条件の読取り](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go)、[条件評価](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)、[提供フィールド](https://github.com/imksoo/routerd/blob/main/pkg/api/provides.go)。
