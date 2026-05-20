# Alpine / OpenRC デプロイ

Alpine Linux では routerd は OpenRC を service manager として扱います。
one-shot apply は routerd 管理のローカルサービスまで含めて自己完結します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

`mode: vrrp` の `VirtualAddress` がある場合、
routerd は `/etc/keepalived/keepalived.conf` を render し、OpenRC の
`keepalived` init script を導入し、`rc-update` で有効化します。config
変更は daemon mode と同じ VRRP controller 経路で適用し、daemon が稼働中なら
`rc-service keepalived reload`、必要な場合は `restart` に fall back します。
生成 script は起動前に `keepalived --config-test --use-file
/etc/keepalived/keepalived.conf` を実行します。

`routerctl show vrrp` の role は live interface state から観測します。
Linux/OpenRC では `ip addr show` で VIP を持っている node を `master`、
持っていない peer を `backup` と判定します。
`LAST_TRANSITION` は routerd または `routerctl show vrrp` がその node の
role 変化を最後に観測した時刻です。keepalived 単独の failover では、CLI が
次に live VIP ownership を読んだタイミングで更新されます。

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

どちらの条件も満たさない場合、Live ISO はログイン時に 5 秒だけ wizard の
開始入力を待ちます。入力が無ければ wizard 経路を終了し、ephemeral mode の
まま動きます。後から開始する場合は
`/usr/share/routerd/install.sh configure` を実行します。

Kubernetes VIP example のように `advertInterval` が 1 秒の構成では、active
node の keepalived を停止すると、おおむね数秒で backup へ VIP が移ります。
keepalived の検出窓は概ね `advertInterval * 3` です。高 priority node への
reclaim は、設定した `preemptDelay` と次の advert convergence window の後に
進みます。
