# ClientPolicy

[リソース別の設定](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

対象クライアント、または例外以外のクライアントへ、ゲスト用の自機サービス・転送宛先制限を適用します。Linuxは設定MAC、FreeBSD PFは明示IPで識別します。

## 指定条件と参照関係

- modeはinclude・excludeが必須です。includeは選択集合、excludeは選択した信頼集合の外側を制限します。Linuxのincludeで対象MACがなければ制限を生成せず、excludeで集合が空なら対象インターフェース上の全クライアントに一致します。
- Linuxの分類はmatch.macsだけを使い、guest・isolatedをinclude、trustedをexcludeの集合に加えます。OUI・ホスト名・指紋は検証されても描画の選別には使いません。ipv4Reservation・ipv6AddressesもLinuxの選別条件にはなりません。
- FreeBSDはトップレベルMAC・全match選別を拒否します。各分類にipv4Reservationまたは明示ipv6Addressesが必要で、PFは予約のMACでなくipAddressを使います。PFのinterfacesは明示してください。省略してもtrustゾーンを引き継ぎません。
- 経路転送・自機入力の制限であり、L2スイッチポート分離ではありません。既存状態・制御・サービス許可が先に動く場合があります。PFはtrust・mgmtからselfへの暗黙許可がクライアント自機制限より先で、自機サービス制限も現在はIPv4識別だけです。両OS・全アドレスファミリーで同一の強制とは扱えません。
- LinuxはguestEgressAllowを探索遮断・拒否より先に評価するため上書きできます。残りはゾーン方針へ進みます。PFのIPv4許可は拒否より先ですが、IPv6拒否は全体の前段に出すため後のIPv6許可では上書きできません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 任意; array
      - # object
        ipv4Reservation: "<string>" # 任意; string
        ipv6Addresses: # 任意; array
          - "<string>" # string
        match: # 必須; object
          dhcpFingerprints: # 任意; array
            - "<string>" # string
          hostnamePatterns: # 任意; array
            - "<string>" # string
          macs: # 任意; array
            - "<string>" # string
          ouiPrefixes: # 任意; array
            - "<string>" # string
        mode: "trusted" # 必須; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 任意; string
    guestEgressAllow: # 任意; array
      - "<string>" # string
    guestEgressDeny: # 任意; array
      - "<string>" # string
    guestServices: # 任意; array
      - "<string>" # string
    interfaces: # 任意; array
      - "<string>" # string
    isolation: # 任意; object
      lanInternet: "" # 任意; string; enum=["", "allow", "deny"]
      lanLAN: "" # 任意; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 任意; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 任意; string; enum=["", "allow", "deny"]
    macs: # 任意; array
      - "<string>" # string
    mode: "include" # 必須; string; enum=["include", "exclude"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.mode` | include・excludeが必須で既定値はありません。識別集合を制限対象にするか、例外にするかを選びます。 |
| `spec.interfaces` | 既存のInterface/nameまたは裸の論理名の一覧で、重複不可です。Linuxの省略時はtrustゾーン全インターフェースを使い、なければエラーです。PFの省略時は適用先がなく制限が生成されません。 |
| `spec.macs` | 任意のLinux固定MAC集合で既定は空です。本一覧・classification.match.macsをまたぐ正規化後の重複は禁止です。FreeBSDは禁止です。 |
| `spec.classification` | 任意の分類一覧で既定は空です。Linuxは各分類で1種類以上の選別が必須ですが、実際の集合にはMACだけを使います。FreeBSDは安定IPが必須で選別条件は禁止です。 |
| `spec.classification[].name` | 任意の説明名で既定は空です。優先順位を変えず、リソース識別子にもなりません。 |
| `spec.classification[].mode` | trusted・guest・isolatedが必須です。includeではguest・isolatedを同じ対象集合に、excludeではtrustedを例外集合に加えます。 |
| `spec.classification[].match` | Linuxでは1件以上の選別一覧が必要です。FreeBSDは安定IPを指定しても空にします。現行ファイアウォールは各種選別を動的にMACへ解決しません。 |
| `spec.classification[].match.macs` | 任意の完全MAC一覧で、Linuxで実効のある選別です。解釈可能なMACとし、分類間・トップレベルとの重複は禁止です。 |
| `spec.classification[].match.ouiPrefixes` | 18:ec:e7のような3個の16進オクテットで、分類内重複は禁止です。現行ファイアウォールの選別効果はなく、FreeBSDは拒否します。 |
| `spec.classification[].match.hostnamePatterns` | 空でないGo path.Match形式のglobです。正規表現ではありません。構文だけを検証し、現行ファイアウォールは識別集合へ解決しません。FreeBSDは拒否します。 |
| `spec.classification[].match.dhcpFingerprints` | 空白を含まない空でないトークンで、分類内重複は禁止です。現行のファイアウォール集合には使わず、FreeBSDは拒否します。 |
| `spec.classification[].ipv4Reservation` | 既存DHCPv4Reservationの裸のmetadata.nameです。Kind/nameは禁止です。PFはその空でないipAddressを使い、Linuxは参照検証だけで選別には使いません。 |
| `spec.classification[].ipv6Addresses` | PF用のCIDRでないIPv6アドレスです。IPv4射影形式は禁止で、全分類を通して正規化後の重複も禁止です。MAC・IPv4予約から推測せず、Linuxの選別には使いません。 |
| `spec.isolation` | 任意のallow・deny設定です。4項目を読むのはLinuxだけで、PFはguestEgressの明示一覧・既定値を使います。分類ごとに独立適用する設定ではありません。 |
| `spec.isolation.lanInternet` | 既定は空です。LinuxのdenyはguestEgressDeny未指定時に拒否集合を0.0.0.0/0・::/0へ置き換えます。allow・空はインターネット無条件許可を追加しません。PFは使いません。 |
| `spec.isolation.lanLAN` | 既定は空です。LinuxはlanLAN・lanMgmtが両方allowのときだけ既定のプライベートCIDR拒否を外し、個別LANを識別しません。guestEgressDeny明示値が既定値に優先します。PFは使いません。 |
| `spec.isolation.lanMgmt` | 既定は空で、lanLANと組み合わせた既定集合の判定にだけ使います。管理ゾーンの宛先を独立に解決しません。PFは使いません。 |
| `spec.isolation.mDNSBroadcast` | 既定は空です。LinuxのdenyはguestEgressAllowより後に、転送mDNS・SSDPマルチキャストとUDP 137・138の拒否を加えます。L2ブリッジ・自機サービスを独立には制御しません。PFは使いません。 |
| `spec.guestServices` | 自機で許可するdns・dhcp・ntp・mdns・ssdpです。省略・空はdhcp・dns・ntpが既定で、空一覧で例外をすべて無効にはできません。通信を開けるだけでデーモンは起動しません。 |
| `spec.guestEgressDeny` | 任意のIPv4・IPv6 CIDRです。省略・空は10.0.0.0/8・172.16.0.0/12・192.168.0.0/16・fc00::/7で、Linuxでは前述のisolationにより調整します。明示値は追加でなく置換です。PFは空なら常にこのプライベート集合です。 |
| `spec.guestEgressAllow` | 任意のIPv4・IPv6 CIDR例外で、既定は空です。Linux・PF IPv4は拒否より先、PF IPv6は拒否より後に評価します。リソース参照でなく転送宛先プレフィックスです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/webconsole/clients.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/clients.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
