---
title: プラグインプロトコル
slug: /reference/plugin-protocol
---

# プラグインプロトコル

routerd のプラグインは、信頼済みのローカル実行ファイルです。
本体に組み込まないリソース固有の処理を、同じホスト上の小さなプログラムとして追加するための仕組みです。

リモートからのプラグイン登録、リモートインストール、公開レジストリは、現在は対象外です。

## 配置

標準の配置先は次の通りです。

```text
/usr/local/libexec/routerd/plugins/<name>/
```

各プラグインはマニフェストと実行ファイルを持ちます。

```text
plugin.yaml
bin/<plugin>
```

## 役割

プラグインは次のような処理を担当できます。

- リソースの検証
- 変更計画の作成
- ホスト状態の観測
- ホストへの適用

ただし、ネットワーク状態を変更する処理は、テストしやすい小さな単位に分けます。
本体と同じく、ホストネットワークを変更するテストは、`tests/netns` などの隔離環境で行います。

## 現在の位置付け

routerd の主要なルーター機能は、本体のリソースと専用デーモンで実装を進めています。
プラグインは、利用者ごとのローカル拡張を安全に取り込むための基盤です。
公開互換 API として固定するまでは、マニフェストと入出力の形が変わる可能性があります。

## CloudEdge MVP

CloudEdge MVP のプラグインは、信頼済みのローカル実行ファイルだけを対象にします。
routerd はリモートレジストリから取得せず、プラグインが返す `actionPlans` も実行しません。
`actionPlans` は表示専用で、クラウド API の変更や secondary private IP の割り当ては
routerd の対象外です。プラグインは `RemoteAddressClaim` などの動的リソース候補を返せますが、
MVP では観測結果として検証・表示されるだけです。

起動設定では `Plugin` と `DynamicConfigSource` を宣言できます。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: oci-inventory
spec:
  executable: /usr/local/libexec/routerd/plugins/oci-inventory/bin/oci-inventory
  timeout: 10s
  capabilities: [observe.cloud, propose.dynamicConfig]
  triggers:
    - type: interval
      every: 300s
```

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata:
  name: oci-inventory
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

routerd はプラグインの標準入力へ `PluginRequest` JSON を 1 つ書き込み、
標準出力から `PluginResult` を 1 つ読み取ります。出力が JSON でも YAML デコーダーで読み取り、
`status.resources` の spec を routerd の型へ復元します。

利用できる CLI は次の通りです。

```text
routerctl plugin list [--config <startup>] [-o table|json|yaml]
routerctl plugin run <name> [--dry-run] [--config <startup>] [--state-file <db>] [-o table|json|yaml]
```

`--dry-run` は候補の `DynamicConfigPart` を表示するだけで、状態 DB へは書き込みません。
