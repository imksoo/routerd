# Alpine / OpenRC 部署

![Diagram showing Alpine and OpenRC deployment from routerd validation and render preview through OpenRC service management, keepalived config testing, live ISO wizard skipping, DHCP renewal, and VRRP status observation](/img/diagrams/operations-alpine-deployment.png)

在 Alpine Linux 上，routerd 以 OpenRC 作為服務管理器。
單次套用（one-shot apply）涵蓋 routerd 管理的本地服務，自給自足。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

若設定中含有 `mode: vrrp` 的 `VirtualAddress`，routerd 會產生（render）`/etc/keepalived/keepalived.conf`，
安裝 OpenRC 的 `keepalived` init script，並以 `rc-update` 啟用。
設定變更透過與常駐程式模式相同的 VRRP 控制器路徑套用。
常駐程式運作中時執行 `rc-service keepalived reload`，必要時退回 `restart`。
產生的 script 在啟動前會執行 `keepalived --config-test --use-file /etc/keepalived/keepalived.conf`。

`routerctl show vrrp` 顯示的 role，是從運作中介面的狀態觀測得到的。
在 Linux / OpenRC 上，以 `ip addr show` 判斷：持有 VIP 的節點為 `master`，對等節點為 `backup`。
`LAST_TRANSITION` 是 routerd 或 `routerctl show vrrp` 最後觀測到該節點 role 變更的時刻。
若僅由 keepalived 單獨執行容錯移轉，CLI 下次讀取到運作中 VIP 擁有權時才會更新。

若要在不變更主機的情況下預覽 Alpine 的輸出，請執行：

```sh
routerd render alpine --config /usr/local/etc/routerd/router.yaml
```

含有 VRRP VIP 的設定，預覽中會包含 OpenRC init script 及 `keepalived.conf`。
有關在同一 VIP 上同時使用 DNS port 53 與 API ingress port 6443 的 Kubernetes API VIP 範例，
請參閱 `examples/k8s-routerd-vip-alpine.yaml`。

在 Live ISO 上，若 `/usr/local/etc/routerd/router.yaml` 已存在，登入時不會啟動精靈。
也可在開機命令列加入以下參數來抑制：

```text
routerd.skip-wizard=1
```

若兩個條件均不成立，Live ISO 在登入時會等待 5 秒讓使用者決定是否啟動精靈。
無輸入則結束精靈流程，以 ephemeral 模式繼續運作。
事後啟動請執行 `/usr/share/routerd/install.sh configure`。

Live ISO 透過 autostart 路徑以 `udhcpc` 作為常駐 DHCP 用戶端啟動，
開機後持續進行租約的 renew/rebind。
DHCP 主機名稱依序從 `routerd.hostname=`、`routerd.live_hostname=`、
頂層 Router 的 `metadata.name`，或 MAC 位址衍生的後備值決定。
預設不傳送 DHCP option 61，因此以 Ethernet MAC 識別用戶端的 DHCP 伺服器，
會以相同的用戶端識別碼處理。
僅在明確需要 DHCP 用戶端 ID 時，才以 hex 值透過 `routerd.dhcp_client_id=` 指定。

如 Kubernetes VIP 範例中 `advertInterval` 為 1 秒的設定，
停止活躍節點的 keepalived 後，通常數秒內 VIP 即移轉至 backup 節點。
keepalived 的偵測視窗約為 `advertInterval * 3`。
優先度較高節點的 reclaim，在設定的 `preemptDelay` 及下一個 advert convergence 視窗後進行。
