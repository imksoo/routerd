---
title: 反映、掃除、削除
---

# 反映、掃除、削除

routerd は kubectl に近い反映モデルを使います。

既定の `routerd apply` は追加型です。

```bash
sudo routerd apply --config wan.yaml --once
sudo routerd apply --config lan-services.yaml --once
```

2 つ目のコマンドは `lan-services.yaml` に書かれたリソースを更新し、
`wan.yaml` で反映したリソースは残します。ルーター設定を複数ファイルに
分ける場合や、段階的に生成する場合はこの使い方を基本にします。

そのファイルを完全な望ましい状態として扱う場合だけ、`--prune` を使います。

```bash
sudo routerd apply --config full-router.yaml --once --prune
```

`--prune` を付けると、今回のファイルに存在しない routerd 所有の構成物を
削除することがあります。遠隔のルーターでは、必ず先に予行実行します。

```bash
sudo routerd apply --config full-router.yaml --once --dry-run --prune
routerctl describe orphans
```

意図して消す場合は、明示的な削除を優先します。

```bash
sudo routerd delete IPv6PrefixDelegation/wan-pd
routerctl delete IPv6PrefixDelegation/wan-pd
```

`routerd delete -f old-resource.yaml` は、その Router YAML に書かれた
リソースをすべて削除対象にします。`routerctl delete` は、起動中のデーモンへ
ローカル制御ソケット経由で 1 つのリソース削除を依頼します。
