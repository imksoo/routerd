# 動態設定

啟動 YAML 仍由使用者管理。經驗證的執行時資料可暫時加入資源，過期後於下次合併排除。請透過資源狀態區分宣告設定與 DHCP/PD/BGP 取得的狀態。

目前 DynamicConfigSource 僅驗證宣告，沒有執行時使用流程。宣告它不會排程 Plugin，也不會以 ttl 設定 PluginResult.status.ttl。EventSubscription 可呼叫本機 Plugin；手動呼叫使用 routerctl plugin run。

Plugin 在結果中傳回 TTL。僅政策允許的完全一致 mask 會在有效期間抑制啟動資源，不改寫 YAML。供應商操作提案受獨立 ProviderActionPolicy 控制，不會只因合併執行時資料而套用。

套用變更前，先檢查有效設定：

```sh
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
```

各欄位的意義與實作限制請見資源說明：

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [EventSubscription](resources/EventSubscription.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
