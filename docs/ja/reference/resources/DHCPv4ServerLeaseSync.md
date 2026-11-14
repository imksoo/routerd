# DHCPv4ServerLeaseSync

[リソース別の設定](../resources.md#dhcpv4serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DHCPv4サーバーのリースファイルと存在する固定保持状態を、SSH経由のrsyncで相手ホストへ複製します。DHCPのフェイルオーバープロトコルではありません。

## 指定条件と参照関係

- source.resourceと1件以上のtarget.hostが必須です。状態値参照とは異なり、通常検証は表記・型だけを確認し、DHCPv4Serverの存在を検証しません。存在しない名前でもプラットフォーム既定リースパスに補完します。
- 宛先パスはローカル送信元と同じ絶対パスで、YAMLに宛先パス指定はありません。名前が一致する送信元のstickyHoldDaysが正なら、存在するdhcp-sticky.dbと-walも複製し、-shmは複製しません。主リースファイルは必須です。
- 各実行はrsync -a --checksum --delay-updatesを使い、既定のrsync無通信制限60秒とコマンド実行制限2分があります。SSH既定はBatchMode=yes・ConnectTimeout=10でsshOptionsから変更できます。Syncedは複製成功であり、相手のリース再読込や通信継続の証明ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    source: # 任意; object
      resource: "<string>" # 必須; string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        options: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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
| `spec.source` | resourceを持つ送信元指定が必須です。ここでファイルパスは上書きできません。 |
| `spec.source.resource` | DHCPv4Serverの裸名またはDHCPv4Server/nameが必須です。そのleaseFileが非空なら使用し、なければ<StateDir>/dnsmasq/dnsmasq.leasesです。状態値参照ではなくfield・optionalはありません。 |
| `spec.command` | 任意の実行ファイル名・パスで、既定rsyncです。シェル文字列ではなくrsync引数を直接受け取ります。 |
| `spec.interval` | Go durationで、既定30sです。前回成功syncedAtからの実行抑制期間で、コントローラーは別途30秒周期と購読イベントで動きます。0・負値は抑制をなくすだけで周期を短縮しません。 |
| `spec.targets` | 1件以上の相手一覧が必須です。ファイルを各相手へ順番に送り、最初の失敗でError/SyncFailedになります。相手routerd APIは呼びません。 |
| `spec.targets[].name` | 任意の状態表示名で、既定空です。宛先ホスト・送信元の選択には使いません。 |
| `spec.targets[].host` | SSHのホスト・アドレスが必須です。CR/LFは禁止で、到達経路と非対話SSH認証を事前に用意します。 |
| `spec.targets[].user` | 任意のSSHユーザーです。空ならuser@を付けずSSH設定のユーザーを使用します。CR/LFは禁止です。 |
| `spec.targets[].sshOptions` | rsync -eへ組み込むSSH引数一覧です。該当キーが未指定なら-o BatchMode=yesと-o ConnectTimeout=10を補います。各項目にCR/LFは使えません。 |
| `spec.targets[].options` | 既定の後に追加するrsync引数です。--timeoutまたは--timeout=...があれば既定--timeout=60を補いません。別の実行制限2分は変更しません。CR/LFは禁止です。 |
| `spec.when` | 共通の状態条件で、既定無条件です。待機側からの書込を防ぐには実際の役割状態で条件を指定します。本リソース自体はリースファイルの主系を合意しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
