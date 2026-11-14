# DHCPv4Reservation

[リソース別の設定](../resources.md#dhcpv4reservation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣言したDHCPv4Serverに対し、MACアドレスへIPv4アドレスを予約します。

## 指定条件と参照関係

- スキーマ上serverが任意でも、server・macAddress・ipAddressは意味上必須です。serverはinterfaceを持つDHCPv4Serverの裸名です。通常検証はサーバー1台でも省略を許しません。scopeは拒否されます。
- 現行実行処理は、リソース別タグを付けた予約を共有dnsmasqホスト補助ファイルへ書きます。serverとの関連は検証・状態・個別オプション生成に使いますが、別プロセスや独立した予約ファイルにはなりません。Appliedはクライアントの取得成功を示しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 任意; string
    ipAddress: "<string>" # 必須; string
    leaseTime: "<string>" # 任意; string
    macAddress: "<string>" # 必須; string
    options: # 任意; array
      - # object
        code: 1 # 任意; integer; minimum=1; maximum=65535
        name: "<string>" # 任意; string
        value: "<string>" # 任意; string
    scope: "<string>" # 任意; string
    server: "<string>" # 任意; string
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
| `spec.scope` | 廃止フィールドで、非空値は拒否されます。serverを使用してください。 |
| `spec.server` | interfaceを指定した既存DHCPv4Serverのmetadata.nameが必須です。Kind/nameの接頭辞はここでは除去しません。 |
| `spec.macAddress` | net.ParseMACで解析できるMACアドレスが必須です。小文字で出力し、既定値はありません。 |
| `spec.hostname` | 空白・カンマを含まない任意ホスト名で、既定は省略です。有効化したDNSZoneのDHCP由来ホスト情報にも利用できます。 |
| `spec.ipAddress` | 予約する裸のIPv4アドレスが必須です。現行検証は系統を確認しますが、サーバープール内所属や予約IP・MACの重複はここで検査しません。 |
| `spec.leaseTime` | 任意のバックエンド期間文字列で、既定空です。カンマは拒否します。現行実行時の予約ホスト行には出力しないため、この経路でリース期間を変更しません。 |
| `spec.options` | 予約タグに対応する任意DHCPオプション一覧です。各項目はcode・nameの片方と非空valueが必要です。既定空です。 |
| `spec.options[].code` | 数値のオプション識別子でnameと排他です。スキーマ範囲1～65535で、既定識別子はありません。 |
| `spec.options[].name` | dnsmasqのオプション名でcodeと排他です。空白・カンマは不可です。 |
| `spec.options[].value` | 非空のバックエンド形式値が必須です。CR/LFは不可で、そのまま出力するためカンマによる複数値指定は可能です。 |
| `spec.when` | 共通の状態条件で、既定無条件です。不成立なら予約を除外しPending/WhenFalse、サーバー利用不可ならServerPendingです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)
