---
title: はじめに
---

# はじめに

このチュートリアルでは、routerd の最小ワークフローを通します。binary を build し、YAML config を validate し、dry-run の plan を確認してから、one-shot reconcile を実行します。

routerd はまだ v1alpha1 のソフトウェアです。実ルーターへ適用する前に、lab VM や console access のあるホストで試してください。

## 1. routerd を build する

```bash
make build
```

生成される binary:

- `bin/routerd`
- `bin/routerctl`

## 2. 小さな config から始める

まず basic static example を validate します。

```bash
routerd validate --config examples/basic-static.yaml
routerd reconcile --config examples/basic-static.yaml --once --dry-run
```

dry-run の出力は JSON status です。どの resource が healthy か、drifted か、routerd が何をしようとしているかを確認できます。

## 3. source install layout で入れる

routerd は `/usr/local` 配下を default install layout としています。

```bash
sudo make install
sudo install -m 0644 examples/basic-static.yaml /usr/local/etc/routerd/router.yaml
```

主な default path:

- Config: `/usr/local/etc/routerd/router.yaml`
- Binary: `/usr/local/sbin/routerd`
- Plugins: `/usr/local/libexec/routerd/plugins`
- Runtime: `/run/routerd`
- State: `/var/lib/routerd`

## 4. one-shot reconcile する

daemon を有効にする前に、必ず one-shot mode で確認します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

plan が期待通りになってから `--dry-run` を外します。

## 5. daemon を有効にする

one-shot 実行が問題なければ systemd unit を入れます。

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` は `/run/routerd/` 配下に local control API socket を開き、定期 reconcile を実行します。

## 次に読むもの

- [リソース API reference](/ja/docs/reference/api-v1alpha1)
- [router lab tutorial](/ja/docs/tutorials/router-lab)
- [control API](/ja/docs/reference/control-api-v1alpha1)
