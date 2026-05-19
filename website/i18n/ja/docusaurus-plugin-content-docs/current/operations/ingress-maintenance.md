---
title: Ingress maintenance
---

# Ingress maintenance

router YAML を編集せずに `IngressService` backend を一時的に外したい場合は、
`routerctl drain` を使います。

```sh
routerctl drain ingress/kubernetes-api backend=cp-01 --duration 10m
routerctl show ingress
```

drain state は routerd state database に保存されます。drain 中、ingress controller は
該当 backend を `drained: true`、`healthy: false`、`reason: Drained` として扱い、
次回 reconcile 以降の新規 flow は残っている healthy backend へ送ります。既存の
conntrack entry は削除しません。

`--duration` が切れると自動で復帰します。すぐに戻す場合は次を実行します。

```sh
routerctl undrain ingress/kubernetes-api backend=cp-01
```
