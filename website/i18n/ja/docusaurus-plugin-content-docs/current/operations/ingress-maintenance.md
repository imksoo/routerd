---
title: Ingress のメンテナンス
---

# Ingress のメンテナンス

![Diagram showing ingress maintenance with routerctl drain writing temporary state, reconcile marking a backend drained and unhealthy, existing conntrack preserved, and undrain or expiry restoring service](/img/diagrams/operations-ingress-maintenance.png)

ルーターの YAML を編集せずに `IngressService` のバックエンドを一時的に外したい場合は、`routerctl drain` を使います。

```sh
routerctl drain ingress/kubernetes-api backend=cp-01 --duration 10m
routerctl show ingress
```

ドレインの状態は routerd の状態データベースに保存します。
ドレイン中、ingress コントローラーは該当バックエンドを `drained: true`、`healthy: false`、`reason: Drained` として扱います。
次回以降の調整（リコンサイル）では、新規フローを残りの healthy なバックエンドへ送ります。
既存の conntrack エントリーは削除しません。

`--duration` が切れると、自動で復帰します。
すぐに戻す場合は、次を実行します。

```sh
routerctl undrain ingress/kubernetes-api backend=cp-01
```
