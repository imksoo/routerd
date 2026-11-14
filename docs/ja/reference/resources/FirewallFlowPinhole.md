# FirewallFlowPinhole

[リソース別の設定](../resources.md#firewallflowpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

ファイアウォールのゾーン組で、IPv4 UDPの戻り許可エンドポイントを学習します。アドレス変換は行いません。Linux nftables専用で、現行PFは生成しません。

## 指定条件と参照関係

- fromZone・toZoneは異なる既存FirewallZoneの裸名が必須で、selfは禁止です。outbound・inboundと送信元選別が必要で、IPAddressSet参照先も必須です。
- 送信条件への一致は期限付き集合を更新して後続へ進み、自動許可ではありません。逆方向は送信元・宛先を入れ替えた制限、相手UDP送信元ポート、学習済み組に一致すると、通常のゾーン間明示ルールより先に許可します。
- 既定remoteAddressは相手IPv4・内部IPv4・内部UDPポートをキーにします。localEndpointはキーから相手を除きますが、設定した相手CIDR・集合は受信送信元にも適用します。送信時の相手UDP宛先ポートは学習キーに含めません。
- 学習要素は一時的なカーネル集合で、ファイアウォールテーブル再作成・再起動で消えます。既存conntrack許可・制御例外が先に評価されるため、全体のファイアウォール方針を置き換えるものではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 任意; object
      key: "" # 任意; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必須; string
    inbound: # 必須; object
      sourcePorts: # 必須; array
        - "<string>" # string
    outbound: # 必須; object
      destinationCIDRs: # 任意; array
        - "<string>" # string
      destinationPorts: # 必須; array
        - "<string>" # string
      destinationSetRefs: # 任意; array
        - "<string>" # string
      protocol: "udp" # 必須; string; enum=["udp"]
      sourceCIDRs: # 任意; array
        - "<string>" # string
      sourceSetRef: "<string>" # 任意; string
    timeout: "<string>" # 任意; string
    toZone: "<string>" # 必須; string
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
| `spec.outbound` | 必須の送信学習条件です。該当UDP通信の識別を記録しますが、送信通信自体のファイアウォール許可ではありません。 |
| `spec.outbound.sourceSetRef` | 既存IPAddressSet/<name>の送信元選別です。sourceCIDRsと少なくとも一方が必須で、両方あればORです。既定は空です。 |
| `spec.outbound.sourceCIDRs` | 任意のIPv4送信元CIDRのOR一覧で、既定は空です。送信元選別が最低1つ必要です。IPv6は拒否し、描画時はホスト部をマスクします。 |
| `spec.outbound.destinationCIDRs` | 学習対象の任意のIPv4相手宛先で、既定は空です。destinationSetRefsとORで組み合わせ、両方なければ相手アドレス不問です。 |
| `spec.outbound.destinationSetRefs` | 任意の既存IPAddressSet/<name>相手宛先参照です。重複は禁止、既定は空です。送信元と宛先の条件群はANDで組み合わせます。 |
| `spec.outbound.protocol` | udpが必須で既定値はありません。この学習機構はTCP・IPv6に対応しません。 |
| `spec.outbound.destinationPorts` | 1件以上必須の送信UDP宛先ポートです。1～65535の数値か、唯一のstart-end範囲を指定します。範囲と他項目の併用は禁止です。 |
| `spec.inbound` | 必須の戻り通信条件です。sourcePortsは相手UDP送信元ポートを選び、ローカル宛先ポートではありません。 |
| `spec.inbound.sourcePorts` | 1件以上必須の相手UDP送信元ポート一覧です。outbound.destinationPortsと同じ1～65535・範囲単独の制約です。 |
| `spec.timeout` | 任意の正のGo形式期間で、既定3mです。該当送信パケットごとに学習要素を更新します。描画は秒単位へ丸め最低1秒にし、UDPプロトコルのタイムアウトでなく学習状態の期限です。 |
| `spec.when` | 共通の有効化条件で、既定は無条件です。除外したリソースの学習マップ・集合ルールは生成しません。入れ子条件とスキーマ制約は共通項を参照してください。 |
| `spec.fromZone` | 送信開始側の既存FirewallZone metadata.nameが必須です。toZoneと異なる必要があり、selfは禁止です。 |
| `spec.toZone` | 送信先側の既存FirewallZone metadata.nameが必須です。戻り判定では入力側になり、selfは禁止です。 |
| `spec.correlation` | 任意の組み合わせキー選択で、既定はremoteAddressです。リソース参照ではありません。 |
| `spec.correlation.key` | remoteAddress（既定）・localEndpointです。前者は相手IPv4をキーに含み、後者は内部IPv4・UDPポートだけです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
