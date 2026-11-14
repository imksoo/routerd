# BFD

[リソース別の設定](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

vtysh経由でFRR bfddのセッションを設定・観測し、BGPPeerの障害処理に使います。GoBGPとは別バックエンドで、BFD DownによりBGPピアをリセットします。

## 指定条件と参照関係

- 宣言したBFDは少なくとも1つのBGPPeer.bfdから参照される必要があります。BFDのpeerは参照元BGPPeer名またはその相手アドレスに一致する必要があり、BGPDynamicPeerにはbfd項目がありません。
- Linux・FreeBSDでFRR bfdd・vtyshが必要です。interfaceなしは参照BGPRouter.routerIDをローカルに使うmultihopで、直接IP指定かつinterfaceなしの場合はBGPRouterがちょうど1件必要です。interfaceありはsingle-hopです。
- BFDはそのセッションの障害を検出するもので、Upでも全宛先の疎通は保証しません。GoBGPは過去のBFD UpまたはBGP確立後のDown観測に反応し、起動時・不明状態をすべてDownとは扱いません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 任意; integer; minimum=1; maximum=50
    interface: "<string>" # 任意; string
    minRx: "<string>" # 任意; string
    minTx: "<string>" # 任意; string
    peer: "<string>" # 必須; string
    profile: "" # 任意; string; enum=["", "fast", "normal", "slow"]
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
| `spec.peer` | IPアドレスそのものかBGPPeer/name参照が必須です。BGPPeer参照は全相手アドレスへ展開し、routerRef先のrouterIDをローカルアドレスに使います。直接指定のホスト名は不可です。 |
| `spec.interface` | 任意のsingle-hopインターフェースです。Interface/nameなら宣言したInterface.ifnameへ解決します。名前だけの場合は論理インターフェースの存在を検証しますが、実行時はその文字列をFRRへ渡すため、Interface/nameを推奨します。省略時はmultihopです。 |
| `spec.profile` | fast・省略はminRx=minTx=300ms・倍率3、normalは500ms・500ms・3、slowは1000ms・1000ms・5です。個別指定があれば優先します。 |
| `spec.minRx` | 任意のGo durationで50ms～60000msです。プロファイルの受信間隔を上書きして整数msに変換し、省略時はプロファイル値です。BFDプロトコルタイマーであり、ルーター再起動の期限ではありません。 |
| `spec.minTx` | 任意のGo durationで50ms～60000msです。プロファイルの送信間隔を上書きし、整数msで出力します。 |
| `spec.detectMultiplier` | 任意の整数1～50です。省略・デコード値0ならプロファイル既定（通常3、slowは5）です。交渉された検出間隔の倍率で、routerdの再調整回数ではありません。 |
| `spec.when` | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
