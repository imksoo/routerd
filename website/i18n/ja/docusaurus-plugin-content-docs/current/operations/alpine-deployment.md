# Alpine / OpenRC デプロイ

![Diagram showing Alpine and OpenRC deployment from routerd validation and render preview through OpenRC service management, keepalived config testing, live ISO wizard skipping, DHCP renewal, and VRRP status observation](/img/diagrams/operations-alpine-deployment.png)

Alpine Linux では、routerd は OpenRC をサービスマネージャーとして扱います。
one-shot apply は、routerd が管理するローカルサービスまで含めて自己完結します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

`mode: vrrp` の `VirtualAddress` がある場合、routerd は `/etc/keepalived/keepalived.conf` を生成（レンダリング）し、OpenRC の `keepalived` init script を導入し、`rc-update` で有効化します。設定の変更は、デーモンモードと同じ VRRP コントローラー経路で適用します。デーモンが稼働中なら `rc-service keepalived reload` を、必要な場合は `restart` にフォールバックします。
生成した script は、起動前に `keepalived --config-test --use-file /etc/keepalived/keepalived.conf` を実行します。

`routerctl show vrrp` の role は、稼働中のインターフェースの状態から観測します。
Linux/OpenRC では、`ip addr show` で VIP を持っているノードを `master`、持っていないピアを `backup` と判定します。
`LAST_TRANSITION` は、routerd または `routerctl show vrrp` が、そのノードの role 変化を最後に観測した時刻です。keepalived 単独のフェイルオーバーでは、CLI が次に稼働中の VIP 所有権を読んだタイミングで更新されます。

ホストを変更せずに Alpine 向けの出力を確認するには、次を使います。

```sh
routerd render alpine --config /usr/local/etc/routerd/router.yaml
```

VRRP VIP を含む設定では、OpenRC の init script と `keepalived.conf` がプレビューに含まれます。Kubernetes API VIP で、同じ VIP 上に DNS の port 53 と API ingress の port 6443 を併用する例は、`examples/k8s-routerd-vip-alpine.yaml` を参照してください。

ライブ ISO では、`/usr/local/etc/routerd/router.yaml` が存在する場合、ログイン時にウィザードを起動しません。ブートコマンドラインに次を入れても抑止できます。

```text
routerd.skip-wizard=1
```

どちらの条件も満たさない場合、ライブ ISO はログイン時に、ウィザードの開始入力を 5 秒だけ待ちます。入力がなければウィザード経路を終了し、ephemeral モードのまま動きます。後から開始する場合は、`/usr/share/routerd/install.sh configure` を実行します。

ライブ ISO は、autostart 経路で `udhcpc` を常駐 DHCP クライアントとして起動し、起動後もリースの renew/rebind を継続します。DHCP のホスト名は、`routerd.hostname=`、`routerd.live_hostname=`、トップレベル Router の `metadata.name`、または MAC アドレス由来のフォールバックから決めます。既定では DHCP option 61 を送らないため、Ethernet の MAC でクライアントを識別する DHCP サーバーでは、同じクライアント識別子のまま扱われます。明示的な DHCP クライアント ID が必要な場合だけ、hex 値を `routerd.dhcp_client_id=` で指定します。

Kubernetes VIP の例のように `advertInterval` が 1 秒の構成では、アクティブなノードの keepalived を停止すると、おおむね数秒で backup へ VIP が移ります。
keepalived の検出窓は、概ね `advertInterval * 3` です。優先度の高いノードへの reclaim は、設定した `preemptDelay` と、次の advert convergence window の後に進みます。
