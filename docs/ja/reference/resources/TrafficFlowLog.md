# TrafficFlowLog

[リソース別の設定](../resources.md#trafficflowlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

接続のスナップショットをSQLiteのフローDBに記録し、利用可能ならDPIキャッシュの情報を補います。パケットキャプチャーや全パケットの記録ではありません。

## 指定条件と参照関係

- 監視処理は無効なものも含め、最初にデコードできたTrafficFlowLogを選びます。後続の有効なリソースでは上書きしません。記録対象を確定するため1件で設定してください。
- 記録はconntrack情報の取得成功後に行い、取得できない回は記録しません。接続一覧の取得上限は10000件です。監視処理の再実行時に記録し、通常は30秒間隔ですが実行オプションによって変わります。
- DPI補完は最初の有効なFirewallEventLogのDBが利用できる場合に、接続情報に一致する1時間以内の分類キャッシュを使います。includeの各フラグは補完成功を保証せず、両方falseでも利用可能なキャッシュから補完します。フローの保持期間はLogRetentionのtrafficFlowsで管理します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ includeApplicationLayer: <boolean> ]
    [ includeTLSSNI: <boolean> ]
    [ path: <string> ]
    [ source: ("" | "conntrack") ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.enabled` | *任意* | boolean | 既定はfalseです。trueは選ばれたリソースの記録を有効にしますが、取得不能な接続情報を利用可能にする指定ではありません。 |
| `spec.path` | **条件付き必須** | string | ルーター上のSQLite DBパスです。通常の設定検証ではenabled時に必須です。読み取り側には/var/lib/routerd/traffic-flows.dbの内部既定値がありますが、enabled時の指定要件は免除されません。 |
| `spec.source` | *任意* | string; enum=["", "conntrack"] | 省略またはconntrackのみを指定でき、実効ラベルの既定はconntrackです。別の収集処理や任意のバックエンドを選ぶ項目ではありません。 |
| `spec.includeApplicationLayer` | *任意* | boolean | 既定はfalseです。enabledとともにtrueにするとDPIヘルパーとLinuxのDPI観測ルールを要求し、nDPIエージェントの利用可否をapplicationLayerとして状態に記録します。libndpiのロードや分類結果の存在は保証しません。 |
| `spec.includeTLSSNI` | *任意* | boolean | 既定はfalseです。enabledとともにtrueにすると同じDPIヘルパー・観測経路を要求します。TLS復号やSNI取得の保証、保存内容の独立した秘匿化・フィルタースイッチではなく、暗号化・未観測の名前は取得できない場合があります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/conntrackobserver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/conntrackobserver/controller.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `path`, `sinks`
