# DS-Lite

AFTRの名前解決にはaftrDNSServersを直接使います。localAddressSourceがdelegatedAddressならlocalDelegatedAddressを指定します。全体例のAFTRとISPのDNS値を実回線に合わせて変更してください。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)

[リソース別のフィールドとYAML](../reference/resources.md)

例のインターフェース名・アドレス・対向設定を変更して使います。まずローカルで検証し、対象ルーターで計画を確認してください。--replaceは全体設定にだけ使います。反映時には管理接続も変わり得るため、コンソールを確保します。

```sh
routerd validate --config example-dslite-home.yaml
```
