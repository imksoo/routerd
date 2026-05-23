---
title: Secret sources
---

# 密钥来源

routerd 支持通过文件或环境变量作为密钥来源，用于 BGP peer 密码及 VRRP/CARP 验证。正式环境配置中，请优先使用下列字段，而非直接在 `password` 或 `authentication` 字段内嵌值：

```yaml
passwordFrom:
  file: /usr/local/etc/routerd/secrets/bgp-password
  base64: false
```

```yaml
authenticationFrom:
  env: ROUTERD_VRRP_AUTH
```

运维注意事项如下：

- 密钥文件请置于以 git 管理的配置目录之外。
- 请配置为 root 拥有、权限模式 `0600` 的文件，或使用服务管理器的凭证机制，确保只有 routerd 能读取该文件。
- 请勿公开在正式主机上生成的 keepalived 或 CARP 配置，因为生成的文件包含已解析的密钥值。
- `base64: true` 是为了通过文件或环境变量传递而使用的编码方式，并非加密。
- `routerd validate` 在引用的密钥文件尚不存在时会显示警告。生成（render）与应用（apply）时，来源必须是可读取的状态。
