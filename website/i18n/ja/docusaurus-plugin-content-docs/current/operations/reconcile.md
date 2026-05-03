---
title: 反映と削除
---

# 反映と削除

routerd は kubectl に近い反映モデルを使います。

既定の `routerd apply` は追加型です。

```bash
sudo routerd apply --config wan.yaml --once
sudo routerd apply --config lan-services.yaml --once
```

2 つ目のコマンドは `lan-services.yaml` に書かれたリソースを更新し、
`wan.yaml` で反映したリソースは残します。ルーター設定を複数ファイルに
分ける場合や、段階的に生成する場合はこの使い方を基本にします。

`apply` は、渡したファイルに書かれていないリソースを削除しません。遠隔の
ルーターで何かを消す前に、残っている構成物を確認します。

```bash
routerctl describe orphans
```

意図して消す場合は、明示的に削除します。

```bash
sudo routerd delete DHCPv6PrefixDelegation/wan-pd
routerctl delete DHCPv6PrefixDelegation/wan-pd
```

`routerd delete -f old-resource.yaml` は、その Router YAML に書かれた
リソースをすべて削除対象にします。`routerctl delete` は、起動中のデーモンへ
ローカル制御ソケット経由で 1 つのリソース削除を依頼します。
