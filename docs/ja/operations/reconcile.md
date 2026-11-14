# 設定の反映と復旧

設定が反映内容の基準です。設定したオブジェクトの欠落や変更を修復し、設定や有効な動的状態に不要なオブジェクトを削除します。routerd自身が作成していないという理由だけで手作業の設定を保護しません。OSの基盤と、動作に必要なDHCP・PD・BGP・SAMの動的状態は静的設定とは区別します。

--replaceには全体設定を渡します。--replaceなしの部分更新では、省略した全リソースを削除するわけではありません。削除計画を確認し、独立したコンソールを確保してください。

```sh
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl get status
routerctl get events --limit 20
```

常駐デーモンが実行中の構成を収束させます。validateやplanの成功だけでは疎通を保証しません。反映後は状態とイベントを確認し、実際の対向との通信を確認します。

```sh
routerd serve --config router.yaml
```

ロールバックは保存済み設定を再反映します。過去のカーネル状態・通信セッション・デーモンのリースを復元するものではありません。

```sh
routerctl rollback --list
routerctl rollback --to <generation>
```

importは選択した物理インターフェースと静的アドレスを明示的に取り込む操作です。結果を全体設定へ統合してから--replaceを使います。取得した動的状態を静的YAMLとして取り込みません。

```sh
routerctl import --interface ens18 --address 192.0.2.1/24 --config router.yaml > candidate.yaml
```

[DB](state-database.md) · [YAML](../reference/resources.md)
