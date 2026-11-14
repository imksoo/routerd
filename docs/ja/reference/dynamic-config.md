# 動的設定

起動時のYAMLは利用者が管理する設定です。検証済みの実行時データは一時的にリソースを追加でき、期限切れになると次の統合から除外されます。宣言した設定とDHCP・PD・BGPで得た状態は、リソースの状態表示で区別します。

DynamicConfigSourceは現行実装では宣言の検証のみで、実行時の利用処理がありません。宣言してもPluginを定期実行せず、ttlもPluginResult.status.ttlを設定しません。EventSubscriptionはローカルのPluginを呼び出せます。手動呼び出しはrouterctl plugin runを使います。

Pluginは結果にTTLを返します。ポリシーで許可した完全一致のmaskだけが、有効期間中に起動設定の対象リソースを抑制し、YAML自体は変更しません。プロバイダー操作案は別のProviderActionPolicyの条件に従い、実行時データの統合だけでは適用しません。

変更を反映する前に、実効設定を確認します。

```sh
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
```

各項目の意味と実装上の制約はリソース別の説明を参照してください。

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [EventSubscription](resources/EventSubscription.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
