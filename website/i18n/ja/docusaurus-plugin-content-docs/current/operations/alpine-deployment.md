# Alpine / OpenRC デプロイ

Alpine Linux では routerd は OpenRC を service manager として扱います。
one-shot apply は routerd 管理のローカルサービスまで含めて自己完結します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

`mode: vrrp` の `VirtualIPv4Address` / `VirtualIPv6Address` がある場合、
routerd は `/etc/keepalived/keepalived.conf` を render し、OpenRC の
`keepalived` init script を導入し、`rc-update` で有効化します。render
結果が変わった場合は `rc-service keepalived restart` を呼びます。生成
script は起動前に `keepalived --config-test --use-file
/etc/keepalived/keepalived.conf` を実行します。

`routerctl show vrrp` の role は live interface state から観測します。
Linux/OpenRC では `ip addr show` で VIP を持っている node を `master`、
持っていない peer を `backup` と判定します。

host を変更せず Alpine 向け出力を確認するには次を使います。

```sh
routerd render alpine --config /usr/local/etc/routerd/router.yaml
```

VRRP VIP を含む config では OpenRC init script と `keepalived.conf` が
preview に含まれます。Kubernetes API VIP で、同じ VIP 上に DNS port 53
と API ingress port 6443 を併用する例は
`examples/k8s-routerd-vip-alpine.yaml` を参照してください。

Live ISO では `/usr/local/etc/routerd/router.yaml` が存在する場合、ログイン
時の wizard は起動しません。boot command line に次を入れても抑止できます。

```text
routerd.skip-wizard=1
```
