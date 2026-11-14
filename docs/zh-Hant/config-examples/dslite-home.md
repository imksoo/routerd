# DS-Lite

AFTR名稱解析直接使用aftrDNSServers。localAddressSource若為delegatedAddress，必須指定localDelegatedAddress。完整範例中的AFTR及ISP DNS值須依實際線路調整。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)

[各資源欄位與YAML](../reference/resources.md)

使用前修改介面名稱、位址與對端設定。先在本機驗證，再於目標路由器檢查計畫。--replace只用於完整設定。套用可能影響管理連線，請保留主控台。

```sh
routerd validate --config example-dslite-home.yaml
```
