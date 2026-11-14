# DHCPv4Reservation

[リソース別の設定](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣言したDHCPv4Serverに対し、MACアドレスへIPv4アドレスを予約します。

## 指定条件と参照関係

- スキーマ上serverが任意でも、server・macAddress・ipAddressは意味上必須です。serverはinterfaceを持つDHCPv4Serverの裸名です。通常検証はサーバー1台でも省略を許しません。scopeは拒否されます。
- 現行実行処理は、リソース別タグを付けた予約を共有dnsmasqホスト補助ファイルへ書きます。serverとの関連は検証・状態・個別オプション生成に使いますが、別プロセスや独立した予約ファイルにはなりません。Appliedはクライアントの取得成功を示しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    [ hostname: <string> ]
    ipAddress: <string>
    [ leaseTime: <string> ]
    macAddress: <string>
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ scope: <string> ]
    server: <string>
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.scope` | *任意* | string | 廃止フィールドで、非空値は拒否されます。serverを使用してください。 |
| `spec.server` | **必須** | string | interfaceを指定した既存DHCPv4Serverのmetadata.nameが必須です。Kind/nameの接頭辞はここでは除去しません。 |
| `spec.macAddress` | **必須** | string | net.ParseMACで解析できるMACアドレスが必須です。小文字で出力し、既定値はありません。 |
| `spec.hostname` | *任意* | string | 空白・カンマを含まない任意ホスト名で、既定は省略です。有効化したDNSZoneのDHCP由来ホスト情報にも利用できます。 |
| `spec.ipAddress` | **必須** | string | 予約する裸のIPv4アドレスが必須です。現行検証は系統を確認しますが、サーバープール内所属や予約IP・MACの重複はここで検査しません。 |
| `spec.leaseTime` | *任意* | string | 任意のバックエンド期間文字列で、既定空です。カンマは拒否します。現行実行時の予約ホスト行には出力しないため、この経路でリース期間を変更しません。 |
| `spec.options` | *任意* | array | 予約タグに対応する任意DHCPオプション一覧です。各項目はcode・nameの片方と非空valueが必要です。既定空です。 |
| `spec.options[].code` | **条件付き必須** | integer; minimum=1; maximum=65535 | 数値のオプション識別子でnameと排他です。スキーマ範囲1～65535で、既定識別子はありません。 |
| `spec.options[].name` | **条件付き必須** | string | dnsmasqのオプション名でcodeと排他です。空白・カンマは不可です。 |
| `spec.options[].value` | *任意* | string | 非空のバックエンド形式値が必須です。CR/LFは不可で、そのまま出力するためカンマによる複数値指定は可能です。 |
| `spec.when` | *任意* | object | 共通の状態条件で、既定無条件です。不成立なら予約を除外しPending/WhenFalse、サーバー利用不可ならServerPendingです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `address`, `hostname`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
