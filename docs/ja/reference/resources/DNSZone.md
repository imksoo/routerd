# DNSZone

[リソース別の設定](../resources.md#dnszone) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DNSForwarder.zoneRefs経由で提供するローカルA/AAAAレコードと自動PTR応答です。任意レコード型・ゾーン転送・署名を備えた汎用権威DNSではありません。

## 指定条件と参照関係

- DNSForwarderから参照し、正引き・逆引きの問い合わせ名をmatchに含めてください。DNSZoneの宣言だけではDNSポートを開きません。該当ゾーンのVirtualAddress・IngressServiceのhostnameも、各externalDNS指定がなければ取り込みます。
- 固定ipv4/ipv6と動的ipv4From/ipv6Fromは併用できます。解決した動的値が同じ系統の固定値を上書きし、未解決なら固定値を保持します。必須参照の未解決は記録単位の待ち情報となり、リゾルバー全体は停止しません。optionalの未解決は待ち情報を記録しません。系統違い・不正な解決値はエラーです。
- dnssec.enabledはローカル応答にADを立てるだけで、RRSIG・DNSKEYの生成や署名検証は行いません。dnssec.mode、dhcpDerived.hostnameSuffix、dhcpDerived.ddns、reverseZonesは現行の応答処理を変えません。署名・DDNS・逆引きゾーン制限として機能すると解釈しないでください。
- A/AAAAと自動生成PTRのみを応答します。存在する名前に対象系統がなければNOERROR/NODATA、ゾーン内の未登録名はNXDOMAINです。ワイルドカード名に対応します。同じ静的hostnameの重複はアドレス追加ではなく後のレコードで置換します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    [ dhcpDerived:
        [ ddns: <boolean> ]
        [ hostnameSuffix: <string> ]
        [ leaseFile: <string> ]
        [ sources:
            - <string>
        ]
        [ ttl: <integer> ]
    ]
    [ dnssec:
        [ enabled: <boolean> ]
        [ mode: ("" | "validate" | "sign") ]
    ]
    [ records:
        -
          hostname: <string>
          [ ipv4: <string> ]
          [ ipv4From:
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
          ]
          [ ipv6: <string> ]
          [ ipv6From:
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
          ]
          [ ttl: <integer> ]
    ]
    [ reverseZones:
        -
          name: <string>
    ]
    [ ttl: <integer> ]
    zone: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.zone` | **必須** | string | home.example.netなどのゾーン名が必須です。実行時に末尾ドットを補い、既定値はありません。検証は空でないことのみのため、通常の小文字DNS名を使用してください。 |
| `spec.ttl` | *任意* | integer; minimum=0 | ゾーンの既定TTL秒数で、省略・0は300です。スキーマ上0以上です。TTL省略の静的レコードと自動PTR応答に使います。 |
| `spec.dnssec` | *任意* | object | ローカル応答のADフラグ設定です。暗号学的な署名・検証は行いません。 |
| `spec.dnssec.enabled` | *任意* | boolean | 既定falseです。trueは署名を生成せず、ローカルの否定・NODATA応答にもADを立てます。 |
| `spec.dnssec.mode` | *任意* | string; enum=["", "validate", "sign"] | スキーマは空・validate・signを許し、既定空です。読み込みますが、現行のローカルゾーン応答では使いません。 |
| `spec.records` | *任意* | array | 静的A/AAAAレコードの一覧で、既定空です。アドレスなしhostnameも検証を通りますがA/AAAA応答を持ちません。 |
| `spec.records[].hostname` | **条件付き必須** | string | 空白・カンマを含まない名前が必須です。相対名にはzoneを付加し、ゾーン内の完全名はそのままです。ゾーン外名にもzoneを付けるため、別ゾーンの公開にはなりません。ワイルドカードは*.subdomainなどです。 |
| `spec.records[].ipv4` | *任意* | string | 任意の裸IPv4アドレスで、既定なしです。ipv4Fromと併用でき、未解決時に残る固定値になります。 |
| `spec.records[].ipv4From` | *任意* | object | 共通の状態値参照で、resource・fieldの明示と存在検証が必要です。先頭の有効値はIPv4必須で、CIDRはホストアドレス部分を使います。実行時は状態だけを読み、他の利用箇所と異なりIPv4StaticAddressの宣言値補完は行いません。 |
| `spec.records[].ipv6` | *任意* | string | 任意の裸IPv6アドレスで、既定なしです。ipv6Fromとの併用・未解決時の補完はIPv4と同じです。 |
| `spec.records[].ipv6From` | *任意* | object | 共通のresource・field・optionalによる参照で、先頭値はIPv6必須です。参照・CIDR・未解決時の規則はipv4Fromと同じで、optional既定falseです。 |
| `spec.records[].ttl` | *任意* | integer; minimum=0 | レコードのTTL秒数です。省略・0はゾーンTTLを継承します。スキーマ上0以上で、DHCP由来のTTLには影響しません。 |
| `spec.dhcpDerived` | *任意* | object | DHCPリースからのレコード生成設定です。有効なゾーンは実行時リースイベントで更新できます。RFC 2136のDDNSではありません。 |
| `spec.dhcpDerived.sources` | *任意* | array | 既定空ではライブリースイベント・保全ホスト情報の取り込みを無効にします。非空なら有効ですが、現行実装は文字列による送信元選別やDHCPリソース参照検証をしません。受信リースイベントを有効な各ゾーンに渡します。 |
| `spec.dhcpDerived.hostnameSuffix` | *任意* | string | 任意文字列として読み込み、既定空です。現行リースレコード生成では使わず、末尾補完はzoneを使います。 |
| `spec.dhcpDerived.ddns` | *任意* | boolean | 既定falseの真偽値として読み込みますが、イベント更新の可否判定にもDNS UPDATE実装にも使いません。 |
| `spec.dhcpDerived.ttl` | *任意* | integer; minimum=0 | DHCP由来の正引きレコードのTTL秒数です。省略・0はゾーンTTLと独立して60です。PTR応答はゾーンTTLを使います。 |
| `spec.dhcpDerived.leaseFile` | *任意* | string | ゾーン構築・再読込時に読むdnsmasqリースファイルです。空ならファイル取込なしで、ライブイベントの有効化とは別です。継続監視するファイルではありません。 |
| `spec.reverseZones` | *任意* | array | 逆引きゾーンの補助情報で、既定空です。現行デーモンは名前を保持してもPTR応答時に参照せず、この一覧によらずレコードからPTRを生成します。 |
| `spec.reverseZones[].name` | **条件付き必須** | string | 各項目内でスキーマ上必須の文字列です。完全逆引きゾーン名として保持しますが、現行の応答選択には影響しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `zone`, `records`, `pendingRecords`, `updatedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
