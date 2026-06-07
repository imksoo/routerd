---
title: 外掛程式協定
slug: /reference/plugin-protocol
---

# 外掛程式協定

routerd 的外掛程式是受信任的本機執行檔。
這是一種機制，可在同一主機上以小型程式的形式新增不內建於核心的資源專用處理邏輯。

目前不支援從遠端登錄外掛程式、遠端安裝或公開登錄檔。

## 部署位置

標準部署路徑如下：

```text
/usr/local/libexec/routerd/plugins/<name>/
```

每個外掛程式包含一個 manifest 與執行檔：

```text
plugin.yaml
bin/<plugin>
```

## 職責

外掛程式可負責下列類型的處理：

- 資源驗證
- 變更計畫建立
- 主機狀態觀測
- 套用至主機

不過，會修改網路狀態的處理應拆分為易於測試的小單元。
與核心相同，修改主機網路的測試應在 `tests/netns` 等隔離環境中進行。

## 目前定位

routerd 的主要路由器功能正持續透過核心資源與專用常駐程式實作。
外掛程式是用於安全整合各使用者本地擴充功能的基礎架構。
在正式固定為公開相容 API 之前，manifest 與輸入輸出格式可能會有所變更。

## CloudEdge MVP

CloudEdge MVP 的外掛程式僅限受信任的本機執行檔。routerd 不會從遠端登錄檔取得外掛程式，
也不會遠端安裝外掛程式。

外掛程式輸出在寫入 dynamic-config 或用於建立 effective-config 之前總會被驗證。外掛程式可提出
resource、directive、provider action plan 與 event。`actionPlans` 在 dynamic-config
內部是 inert 的；plugin runner 與 merge path 不會執行它們。operator 可將其匯入
provider-action journal，只有在 `ProviderActionPolicy`、approval、allowlist 與
dry-run/live mode gate 通過後，才會交給 executor plugin。

![dynamic config 圖：trusted local plugin observation 進入 DynamicConfigPart，inert provider action plan 透過 gated action journal 與 executor plugin path 處理](/img/diagrams/dynamic-config-provider-actions.png)

可用 capability 包括 `observe.cloud`、`observe.providerPrivateIPs`、
`propose.dynamicConfig`、`propose.providerAction`、`execute.providerAction`。
executor plugin 不會從 routerd core 接收 cloud credential；它在自己的程序中使用
cloud-native identity 或自身環境認證。

常用 CLI：

```text
routerctl plugin list [--config <startup>] [-o table|json|yaml]
routerctl plugin run <name> [--dry-run] [--config <startup>] [--state-file <db>] [-o table|json|yaml]
routerctl action import|list|show|approve|execute|journal|rollback ...
```
