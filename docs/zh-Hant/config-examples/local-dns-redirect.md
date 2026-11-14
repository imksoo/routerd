# 轉送至本機服務

LocalServiceRedirect將符合條件的用戶端傳入流量轉送至本機埠，不會啟動接聽服務。請另外設定DNSResolver等服務；destinationSetRef參照IPAddressSet。這不是通用HTTP/HTTPS代理。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)

[各資源欄位與YAML](../reference/resources.md)

使用前修改介面名稱、位址與對端設定。先在本機驗證，再於目標路由器檢查計畫。--replace只用於完整設定。套用可能影響管理連線，請保留主控台。

```sh
routerd validate --config example-local-dns-redirect.yaml
```
