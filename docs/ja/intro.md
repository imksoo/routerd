# routerdの使い方

routerdはYAMLに宣言したネットワーク構成をホストへ反映します。全体の設定をGitで管理し、アドレスを重複記述する代わりに名前付きリソースを参照します。DHCP・PD・BGPで取得する状態は静的設定として固定しません。

- [インストールと更新](install-and-upgrade.md)
- [設定の考え方](concepts/design-philosophy.md)
- [リソース別のフィールドとYAML](reference/resources.md)
- [設定例](config-examples/index.md)
- [運用](operations/reconcile.md)

例のインターフェース名・アドレス・対向設定を変更して使います。まずローカルで検証し、対象ルーターで計画を確認してください。--replaceは全体設定にだけ使います。反映時には管理接続も変わり得るため、コンソールを確保します。
