---
title: Dynamic config
slug: /reference/dynamic-config
---

# Dynamic config

Dynamic config 是一種機制，允許受信任的本機來源在不編輯 startup-config 的情況下提供
執行時 intent。routerd 從 startup YAML、作用中的 dynamic part 和作用中的 mask 中導出
一個 effective-config。effective-config 是唯一的 reconcile 對象。

本頁說明面向 CloudEdge MVP 的 dynamic-config API 形狀。plugin runner 可以將驗證過的
plugin 輸出儲存為 `DynamicConfigPart` 記錄。源自 `actionPlans` 的 provider action
在 dynamic config 中保持不活躍狀態，不會合併到 effective config 中。獨立的
provider-action 引擎僅從作用中的 part 匯入，並僅在通過 `ProviderActionPolicy`、
核准和 executor-plugin 閘控後才執行。

![展示 startup config、DynamicOverridePolicy、受信任的本機 plugin 輸出、DynamicConfigPart、effective config、不活躍 actionPlans、action journal、帶閘控的 executor plugin 路徑的 Dynamic config 示意圖](/img/diagrams/dynamic-config-provider-actions.png)

## DynamicConfigPart

`DynamicConfigPart` 是來自 dynamic 來源的已驗證執行時片段。來源可以提供一般的
`api.Resource` 物件和指令。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicConfigPart
metadata:
  name: oci-inventory
spec:
  source: Plugin/oci-inventory
  generation: 12
  observedAt: "2026-05-29T12:00:00Z"
  expiresAt: "2026-05-29T12:05:00Z"
  digest: sha256:...
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata: { name: app-10-0-1-123 }
      spec:
        domainRef: cloudedge-same-subnet
        address: 10.0.1.123/32
        ownerSide: cloud
        capture: { type: provider-secondary-ip, providerRef: oci-prod, providerMode: vnic-private-ip, nicRef: ocid1.vnic.oc1..example }
        delivery: { peerRef: cloud-main, mode: route, tunnelInterface: wg-hybrid }
  directives:
    - op: mask
      target: { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
      reason: "RemoteAddressClaim/app-10-0-1-123 is active"
```

| 欄位 | 含義 |
| --- | --- |
| `spec.source` | 穩定的來源識別碼。例：`Plugin/oci-inventory`。 |
| `spec.generation` | 單調遞增的來源世代號。用於說明和排序。 |
| `spec.observedAt` | 來源觀測輸入的 RFC3339 時間。 |
| `spec.expiresAt` | 此 part 變為不活躍的 RFC3339 時間。 |
| `spec.digest` | 已驗證 part 酬載的摘要。 |
| `spec.resources` | 在作用期間提供給 effective-config 的資源。 |
| `spec.directives` | 合併指令。目前僅支援 `op: mask`。 |
| `spec.actionPlans` | provider action 提案。不是資源。provider-action 引擎僅從作用中的 part 匯入，並在執行前套用自身的閘控。 |

plugin 透過 `PluginResult.status.ttl` 回傳 TTL duration。routerd 將其相對於
`observedAt` 解析，得到儲存的 `expiresAt`。

## DynamicConfigSource

`DynamicConfigSource` 是將一個 plugin 繫結到 dynamic config 產生的 startup-config
策略。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata: { name: oci-inventory }
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

MVP 的合併策略僅支援 `conflict: reject`。

## DynamicConfigDirective

MVP 支援以下指令操作。

| 操作 | 含義 |
| --- | --- |
| `mask` | 當指令作用時，抑制符合的 startup-config 資源。 |

指令的目標透過 `apiVersion`、`kind`、`name` 識別。目標刻意採用精確比對。
萬用字元 mask 不在 MVP 範圍內。

## DynamicOverridePolicy

`DynamicOverridePolicy` 授權來源對選定的資源使用 dynamic 指令。plugin 可以提議 mask，
但 mask 僅在策略允許該來源、操作和目標時才會生效。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicOverridePolicy
metadata: { name: allow-cloud-plugin-mask }
spec:
  allow:
    - source: Plugin/oci-inventory
      operations: [mask]
      targets:
        - { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
```

策略是 startup-config 的 intent。dynamic 來源不能為自身授予覆寫權限。

## 合併演算法

effective-config 的合併是確定性的。

1. 讀取並驗證 startup-config。
2. 從狀態資料庫讀取已驗證的 dynamic part。
3. 捨棄 `expiresAt` 在合併時間之前的 dynamic part。
4. 將作用中的 dynamic part 按 `source`、然後 `generation`、然後 `metadata.name`
   排序，實現穩定的算繪和診斷。
5. 將作用中的指令與 `DynamicOverridePolicy` 進行比對評估。
6. 將被允許的作用中 mask 所針對的 startup 資源標記為已抑制。
7. 從未被抑制的 startup 資源和作用中的 dynamic 資源建構 effective-config。
8. 在 reconcile 或 dry-run 輸出之前驗證產生的 effective-config。

衝突規則：

- dynamic 資源不得取代具有相同 `apiVersion`、`kind`、`metadata.name` 的
  startup 資源。
- 具有相同 identity 的兩個作用中 dynamic 資源會產生衝突，除非後續定義了
  來源特定的擁有權規則。
- 未被許可的指令在合併中被忽略，並作為驗證或診斷發現報告。
- 過期的 dynamic part 不提供資源，也不提供 mask。

## mask 語意

mask 是抑制而非刪除。startup YAML 不會被修改，git 歷史仍由維運人員擁有，
當所有符合的作用中 mask 過期或被移除時，靜態資源將重新變為作用中。

被抑制的資源應顯示如下狀態。

```yaml
status:
  phase: Suppressed
  maskedBy:
    - Plugin/oci-inventory#12
  maskedUntil: "2026-05-29T12:05:00Z"
```

當多個 mask 針對同一資源時，資源將保持被抑制狀態直到最後一個作用中 mask 過期。
`maskedBy` 列出所有作用中的來源和世代號，`maskedUntil` 是作用中 mask 中最晚的
`expiresAt`。

MVP 的過期行為是 `onExpire=restoreStatic`。當 mask 過期時，routerd 會在下次合併時
將 startup-config 資源還原到 effective-config 中。由於 startup 資源未被修改，
不需要破壞性的清理步驟。

## CLI

目前面向維運人員的介面如下。

```text
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
routerctl plugin run <name> [--dry-run]
```

`dynamic list` 顯示作用中和過期的 part。`dynamic describe` 說明來源、世代號、摘要、
資源、指令和有效期限。`dynamic render` 輸出 effective-config。`dynamic diff` 比較
startup-config 和 effective-config。`plugin run --dry-run` 在不寫入狀態資料庫的
情況下輸出候選的 dynamic part。

參見[混合雲邊緣設計](../design-hybrid-cloud-edge)和
[Plugin protocol](/docs/reference/plugin-protocol)。
