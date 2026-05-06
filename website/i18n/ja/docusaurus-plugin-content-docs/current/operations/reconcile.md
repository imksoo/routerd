---
title: Reconcile と削除
---

# Reconcile と削除

routerd は、YAML が宣言する意図とホスト現在状態を比べます。
差があれば計画 (plan) を作り、必要なら dry-run で確認してから apply します。

## 標準シーケンス

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

遠隔ルーターでは、本番 `apply` の前に管理経路 (SSH、コンソール、ハイパーバイザーコンソール) が変更を生き残ることを確認してください。

## 常駐モード

```bash
routerd serve --config router.yaml
```

serve モードでは、bus 上のイベントに反応して影響範囲のリソースだけを再評価します。
入力は DHCPv6-PD renewal、health check 結果、derived event、inotify による設定変更検知などです。

## 削除

routerd は所有を確認できる artefact (routerd が以前作成、または明示的に adopt したもの) のみ削除します。
第三者構成や手動変更は触りません。

過去の設定への完全 rollback は現状の対象外です。
削除を含む変更の場合は、必ず `routerd plan` と `routerd apply --dry-run` で削除リストを確認してから適用してください。

## 関連項目

- [状態と所有権](../concepts/state-and-ownership.md)
- [Apply と render](../concepts/apply-and-render.md)
- [トラブルシューティング](../how-to/troubleshooting.md)
