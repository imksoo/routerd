---
title: Secret sources
---

# Secret sources

routerd は BGP peer password と VRRP/CARP authentication で file / environment
secret source を使えます。本番設定では inline の `password` や
`authentication` より、次の field を優先してください。

```yaml
passwordFrom:
  file: /usr/local/etc/routerd/secrets/bgp-password
  base64: false
```

```yaml
authenticationFrom:
  env: ROUTERD_VRRP_AUTH
```

運用上の注意:

- secret file は Git 管理される config directory の外に置きます。
- root 所有、mode `0600` の file、または routerd だけに file を見せる
  service-manager credential mechanism を使います。
- 本番 host から生成済み keepalived、CARP config を公開しないでください。
  生成済み file には解決後の secret value が入ります。
- `base64: true` は file/env transport のための encoding であり、暗号化ではありません。
- `routerd validate` は参照先 secret file がまだ存在しない場合に warning を出します。
  render/apply では source が読める必要があります。
