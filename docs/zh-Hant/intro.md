# routerd使用說明

routerd將YAML宣告的網路設定套用至主機。完整設定以Git管理，透過具名資源互相參照，避免重複位址。DHCP、PD及BGP取得的動態狀態不應固定成靜態設定。

- [安裝與更新](install-and-upgrade.md)
- [設定原則](concepts/design-philosophy.md)
- [各資源欄位與YAML](reference/resources.md)
- [設定範例](config-examples/index.md)
- [操作](operations/reconcile.md)

使用前修改介面名稱、位址與對端設定。先在本機驗證，再於目標路由器檢查計畫。--replace只用於完整設定。套用可能影響管理連線，請保留主控台。
