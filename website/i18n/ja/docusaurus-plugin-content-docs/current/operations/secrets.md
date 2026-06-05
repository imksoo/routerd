---
title: シークレットソース
---

# シークレットソース

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
- root 所有で mode `0600` のファイルにするか、routerd だけにファイルを見せるサービスマネージャーのクレデンシャル機構を使います。
- 本番ホストで生成した keepalived や CARP の設定を公開しないでください。生成したファイルには、解決後のシークレット値が入っています。
- `base64: true` は、ファイルや環境変数で受け渡すためのエンコーディングであり、暗号化ではありません。
- `routerd validate` は、参照先のシークレットファイルがまだ存在しない場合に警告を出します。render と apply では、ソースが読める必要があります。
