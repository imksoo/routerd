---
title: 調整と削除
---

# 調整と削除

routerd は、YAML の意図とホストの現在状態を比べます。
差がある場合、必要な変更を計画し、予行実行または実適用します。

## 基本順序

```bash
routerd validate --config router.yaml
routerd plan --config router.yaml
routerd apply --config router.yaml --once --dry-run
routerd apply --config router.yaml --once
```

遠隔ルーターでは、実適用前に管理用接続が残ることを確認します。

## 常駐モード

```bash
routerd serve --config router.yaml
```

常駐モードでは、routerd はイベントを受けて対象リソースを再評価します。
DHCPv6-PD の更新、ヘルスチェック結果、DerivedEvent などが入力になります。

## 削除

routerd は、所有元が分かる構成物だけを削除対象にします。
知らない設定や手作業で作った設定を勝手に消しません。

完全なロールバックは現在の対象外です。
削除を伴う変更では、計画と予行実行を必ず確認してください。
