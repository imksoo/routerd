---
title: 秘密情報の取得元
---

# 秘密情報の取得元

![Diagram showing secret sources referenced from YAML through file or environment providers, root-owned host storage or USB persistence, validation warnings, and render or apply requiring readable secrets](/img/diagrams/operations-secrets.png)

routerd は、BGP ピアのパスワードと VRRP/CARP の認証に、ファイルまたは環境変数から秘密情報を取得できます。
本番設定では、インラインの `password` や `authentication` よりも、次のフィールドを優先してください。

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

- 秘密情報のファイルは、git で管理する設定ディレクトリの外に置きます。
- ホストローカルな秘密情報ファイルの標準配置先は `/usr/local/etc/routerd/secrets/` です。
- root 所有で mode `0600` のファイルにするか、routerd だけにファイルを見せるサービスマネージャーの認証情報機構を使います。
- 本番ホストで生成した keepalived や CARP の設定を公開しないでください。生成したファイルには、展開済みの秘密情報が入っています。
- `base64: true` は、ファイルや環境変数で受け渡すためのエンコーディングであり、暗号化ではありません。
- `routerctl validate` は、参照先の秘密情報ファイルがまだ存在しない場合に警告を出します。render と apply では、取得元が読み取り可能である必要があります。

ライブ ISO で USB 永続化を使う場合、`/usr/local/etc/routerd/secrets` 配下のファイルは `live-persistence.sh save-config` と `flush` により永続化デバイスの `routerd/secrets/` へコピーされます。
起動時には `router.yaml` を apply する前に復元します。
ホスト固有の `routerd/hosts/<hostname>/secrets/` と `routerd/hosts/<mac>/secrets/` は、汎用の `routerd/secrets/` より優先されます。
