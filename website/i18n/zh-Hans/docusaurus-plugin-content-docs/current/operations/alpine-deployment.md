# Alpine / OpenRC 部署

![Diagram showing Alpine and OpenRC deployment from routerd validation and render preview through OpenRC service management, keepalived config testing, live ISO wizard skipping, DHCP renewal, and VRRP status observation](/img/diagrams/operations-alpine-deployment.png)

在 Alpine Linux 上，routerd 以 OpenRC 作为服务管理器。
单次应用（one-shot apply）涵盖 routerd 管理的本地服务，自给自足。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

若配置中含有 `mode: vrrp` 的 `VirtualAddress`，routerd 会生成（render）`/etc/keepalived/keepalived.conf`，
安装 OpenRC 的 `keepalived` init script，并以 `rc-update` 启用。
配置变更通过与守护进程模式相同的 VRRP 控制器路径应用。
守护进程运行中时执行 `rc-service keepalived reload`，必要时退回 `restart`。
生成的 script 在启动前会执行 `keepalived --config-test --use-file /etc/keepalived/keepalived.conf`。

`routerctl show vrrp` 显示的 role，是从运行中接口的状态观测得到的。
在 Linux / OpenRC 上，以 `ip addr show` 判断：持有 VIP 的节点为 `master`，对等节点为 `backup`。
`LAST_TRANSITION` 是 routerd 或 `routerctl show vrrp` 最后观测到该节点 role 变更的时刻。
若仅由 keepalived 单独执行故障转移，CLI 下次读取到运行中 VIP 拥有权时才会更新。

若要在不变更主机的情况下预览 Alpine 的输出，请执行：

```sh
routerd render alpine --config /usr/local/etc/routerd/router.yaml
```

含有 VRRP VIP 的配置，预览中会包含 OpenRC init script 及 `keepalived.conf`。
有关在同一 VIP 上同时使用 DNS port 53 与 API ingress port 6443 的 Kubernetes API VIP 示例，
请参阅 `examples/k8s-routerd-vip-alpine.yaml`。

在 Live ISO 上，若 `/usr/local/etc/routerd/router.yaml` 已存在，登录时不会启动向导。
也可在启动命令行加入以下参数来抑制：

```text
routerd.skip-wizard=1
```

若两个条件均不成立，Live ISO 在登录时会等待 5 秒让用户决定是否启动向导。
无输入则结束向导流程，以 ephemeral 模式继续运作。
事后启动请执行 `/usr/share/routerd/install.sh configure`。

Live ISO 通过 autostart 路径以 `udhcpc` 作为常驻 DHCP 客户端启动，
启动后持续进行租约的 renew/rebind。
DHCP 主机名依次从 `routerd.hostname=`、`routerd.live_hostname=`、
顶层 Router 的 `metadata.name`，或 MAC 地址派生的后备值决定。
默认不发送 DHCP option 61，因此以 Ethernet MAC 识别客户端的 DHCP 服务器，
会以相同的客户端标识符处理。
仅在明确需要 DHCP 客户端 ID 时，才以 hex 值通过 `routerd.dhcp_client_id=` 指定。

如 Kubernetes VIP 示例中 `advertInterval` 为 1 秒的配置，
停止活跃节点的 keepalived 后，通常数秒内 VIP 即迁移至 backup 节点。
keepalived 的检测窗口约为 `advertInterval * 3`。
优先度较高节点的 reclaim，在配置的 `preemptDelay` 及下一个 advert convergence 窗口后进行。
