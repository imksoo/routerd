# DNSUpstream

[リソース別の設定](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DNSForwarderが参照するDNSサーバーの接続先です。宣言だけで待受を開始したり問い合わせを転送したりはしません。

## 指定条件と参照関係

- 内部URL生成にはudpの補完がありますが、設定ではprotocolが必須です。addressまたはaddressFromの1つ以上が必要で、併用時は接続先を結合します。動的addressFromは現行UDPのみです。
- 必須addressFromが未解決なら固定アドレスがあっても、それを含む転送規則全体を見送ります。optionalの未解決値は省略します。bootstrapは異なり、固定一覧があれば必須の動的bootstrapの未解決を補えます。
- sourceInterfaceは論理名・Kind/nameから変換せず、そのままソケットに渡します。Linuxではmetadata.nameとOSのifnameが一致する宣言を使ってください。FreeBSDのソケット処理はifname直接指定を拒否し、通常設定の検証もここで利用できるfib:<n>の代替指定を提供していません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 任意; string
    addressFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    bootstrap: # 任意; array
      - "<string>" # string
    bootstrapFrom: # 任意; array
      - # object
        field: "<string>" # 必須; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    path: "<string>" # 任意; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    protocol: "udp" # 必須; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # 任意; string
    tlsName: "<string>" # 任意; string
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
| `spec.protocol` | udp・tcp・dot・dohのいずれかが必須です。小文字の正式表記を使用します。UDP/TCPは平文DNS、DoTはTLS、DoHはHTTPSです。 |
| `spec.address` | スキーム・ポート・パスを含まないサーバーIPまたはホスト名です。addressFromが空なら必須です。暗黙のサーバーはなく、bootstrap指定時は名前解決に使用します。 |
| `spec.addressFrom` | UDP専用の状態値参照一覧です。各項目に設定内のresourceと提供フィールドの明示が必要で、optional既定falseです。動的な裸アドレスは本リソースのportとは独立してUDP 53へ正規化します。複数サーバーの返却も可能です。 |
| `spec.port` | 1～65535です。省略・0はudp/tcpで53、dotで853、dohで443です。固定addressに適用し、addressFromが返す裸値には適用しません。 |
| `spec.path` | DoHのURLパスで、既定は/dns-queryです。dohで指定する場合は/始まりが必須です。他プロトコルでは使いません。 |
| `spec.tlsName` | DoTの証明書検証・SNI用サーバー名です。省略時は接続先ホスト名を使用します。DoHでは明示指定が検証で拒否されます。UDP/TCPは検証で許しても使用しません。 |
| `spec.bootstrap` | 上流ホスト名の解決用DNS一覧です。bootstrapFromも空・省略で必須依存待ちがなければOSのリゾルバーを使います。同じ転送規則内の上流で結合した設定を共有します。 |
| `spec.bootstrapFrom` | field必須の状態値参照一覧です。resource・fieldの存在が必要で、optional既定falseです。動的・固定サーバーを結合し、固定bootstrapがあれば必須の動的値が未解決でも利用できます。 |
| `spec.sourceInterface` | 宣言したInterfaceまたはWireGuardInterfaceの名前です。既定空ならOSの経路選択に任せます。転送プール単位で共有し、裸のOS名・FreeBSDに関する上記制限があります。 |
| `spec.when` | 共通の状態条件です。既定は無条件で、除外されると転送規則に有効な接続先がなくなる場合があります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
