---
title: シークレットソース
---

# シークレットソース

![Diagram showing secret sources referenced from YAML through file or environment providers, root-owned host storage or USB persistence, validation warnings, and render or apply requiring readable secrets](/img/diagrams/operations-secrets.png)

routerd は、BGP ピアのパスワードと、VRRP/CARP の認証に、ファイルまたは環境変数のシークレットソースを使えます。本番設定では、inline の `password` や `authentication` よりも、次のフィールドを優先してください。

```yaml
passwordFrom:
  file: /usr/local/etc/routerd/secrets/bgp-password
  base64: false
```

```yaml
authenticationFrom:
  env: ROUTERD_VRRP_AUTH
```

運用上の注意は次のとおりです。

- シークレットファイルは、git で管理する設定ディレクトリの外に置きます。
- host-local secret file の標準配置先は `/usr/local/etc/routerd/secrets/` です。
- root 所有で mode `0600` のファイルにするか、routerd だけにファイルを見せるサービスマネージャーのクレデンシャル機構を使います。
- 本番ホストで生成した keepalived や CARP の設定を公開しないでください。生成したファイルには、解決後のシークレット値が入っています。
- `base64: true` は、ファイルや環境変数で受け渡すためのエンコーディングであり、暗号化ではありません。
- `routerctl validate` は、参照先のシークレットファイルがまだ存在しない場合に警告を出します。render と apply では、ソースが読める必要があります。

Live ISO で USB 永続化を使う場合、`/usr/local/etc/routerd/secrets` 配下の file は
`live-persistence.sh save-config` と `flush` により永続化 device の
`routerd/secrets/` へコピーされます。起動時には `router.yaml` を apply する前に復元します。
host-specific な `routerd/hosts/<hostname>/secrets/` と
`routerd/hosts/<mac>/secrets/` は、generic な `routerd/secrets/` より優先されます。
