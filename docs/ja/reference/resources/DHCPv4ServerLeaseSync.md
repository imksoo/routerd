# DHCPv4ServerLeaseSync

[リソース別の設定](../resources.md#dhcpv4serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DHCPv4サーバーのリースファイルと存在する固定保持状態を、SSH経由のrsyncで相手ホストへ複製します。DHCPのフェイルオーバープロトコルではありません。

## 指定条件と参照関係

- source.resourceと1件以上のtarget.hostが必須です。状態値参照とは異なり、通常検証は表記・型だけを確認し、DHCPv4Serverの存在を検証しません。存在しない名前でもプラットフォーム既定リースパスに補完します。
- 宛先パスはローカル送信元と同じ絶対パスで、YAMLに宛先パス指定はありません。名前が一致する送信元のstickyHoldDaysが正なら、存在するdhcp-sticky.dbと-walも複製し、-shmは複製しません。主リースファイルは必須です。
- 各実行はrsync -a --checksum --delay-updatesを使い、既定のrsync無通信制限60秒とコマンド実行制限2分があります。SSH既定はBatchMode=yes・ConnectTimeout=10でsshOptionsから変更できます。Syncedは複製成功であり、相手のリース再読込や通信継続の証明ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    [ command: <string> ]
    [ interval: <string> ]
    source:
      resource: <string>
    targets:
      -
        host: <string>
        [ name: <string> ]
        [ options:
            - <string>
        ]
        [ sshOptions:
            - <string>
        ]
        [ user: <string> ]
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
| `spec.source` | **必須** | object | resourceを持つ送信元指定が必須です。ここでファイルパスは上書きできません。 |
| `spec.source.resource` | **必須** | string | DHCPv4Serverの裸名またはDHCPv4Server/nameが必須です。そのleaseFileが非空なら使用し、なければ<StateDir>/dnsmasq/dnsmasq.leasesです。状態値参照ではなくfield・optionalはありません。 |
| `spec.command` | *任意* | string | 任意の実行ファイル名・パスで、既定rsyncです。シェル文字列ではなくrsync引数を直接受け取ります。 |
| `spec.interval` | *任意* | string | Go durationで、既定30sです。前回成功syncedAtからの実行抑制期間で、コントローラーは別途30秒周期と購読イベントで動きます。0・負値は抑制をなくすだけで周期を短縮しません。 |
| `spec.targets` | **必須** | array | 1件以上の相手一覧が必須です。ファイルを各相手へ順番に送り、最初の失敗でError/SyncFailedになります。相手routerd APIは呼びません。 |
| `spec.targets[].name` | *任意* | string | 任意の状態表示名で、既定空です。宛先ホスト・送信元の選択には使いません。 |
| `spec.targets[].host` | **必須** | string | SSHのホスト・アドレスが必須です。CR/LFは禁止で、到達経路と非対話SSH認証を事前に用意します。 |
| `spec.targets[].user` | *任意* | string | 任意のSSHユーザーです。空ならuser@を付けずSSH設定のユーザーを使用します。CR/LFは禁止です。 |
| `spec.targets[].sshOptions` | *任意* | array | rsync -eへ組み込むSSH引数一覧です。該当キーが未指定なら-o BatchMode=yesと-o ConnectTimeout=10を補います。各項目にCR/LFは使えません。 |
| `spec.targets[].options` | *任意* | array | 既定の後に追加するrsync引数です。--timeoutまたは--timeout=...があれば既定--timeout=60を補いません。別の実行制限2分は変更しません。CR/LFは禁止です。 |
| `spec.when` | *任意* | object | 共通の状態条件で、既定無条件です。待機側からの書込を防ぐには実際の役割状態で条件を指定します。本リソース自体はリースファイルの主系を合意しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
