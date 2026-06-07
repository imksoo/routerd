# High availability

![Diagram showing high availability with RouterdCluster file lease leader election gating routerd mutation while keepalived or CARP separately owns VIP address movement](/img/diagrams/operations-high-availability.png)

`RouterdCluster` 透過輕量的檔案式租約，控制產生器與套用處理的行為，與 VIP 的擁有權相互獨立。VIP 由哪台路由器持有由 keepalived 或 CARP 決定，routerd 則透過租約決定哪個節點可以變更主機設定。

領導節點持有 `spec.leasePath` 的排他鎖，並在 `spec.leaseTTL` 到期前更新租約。待機節點持續執行控制器鏈以供觀測，但會強制讓變更狀態的控制器以 dry-run 模式運作。在單次套用模式下，照常建立計畫並記錄叢集狀態，但跳過 apply。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: RouterdCluster
metadata:
  name: edge-ha
spec:
  peers:
    - routerd-01.lain.local
    - routerd-02.lain.local
  leaseTTL: 30s
  leasePath: /var/lib/routerd/ha-lease
```

若要在同一主機上只保留一個 routerd 處理程序，本地路徑即已足夠。
若要在多台主機間選出單一套用處理程序，請使用 advisory lock 能正確運作的共用檔案系統路徑。

最小設定範例請參閱 `examples/ha-2-node.yaml`。
