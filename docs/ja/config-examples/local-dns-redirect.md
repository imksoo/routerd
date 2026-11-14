# ローカルサービスへの転送

LocalServiceRedirectは条件に一致するクライアントからの受信通信をローカルポートへ転送します。待受サービスは起動しません。DNSResolverなどを別途設定し、destinationSetRefにはIPAddressSetを指定します。汎用HTTP/HTTPSプロキシではありません。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)

[リソース別のフィールドとYAML](../reference/resources.md)

例のインターフェース名・アドレス・対向設定を変更して使います。まずローカルで検証し、対象ルーターで計画を確認してください。--replaceは全体設定にだけ使います。反映時には管理接続も変わり得るため、コンソールを確保します。

```sh
routerd validate --config example-local-dns-redirect.yaml
```
