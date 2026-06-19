---
title: Cloud-init bootstrap design
---

# Cloud-init bootstrap design

このメモは、Proxmox VE、AWS、Azure、OCI 上の routerd ノードで共通に使う
bootstrap contract の案です。PR #546 の Alpine/OpenRC 向けの形は、現在の
Live ISO にはそのまま持ち込みません。現在の対象は Ubuntu `debootstrap`
ベースの Live ISO と systemd first boot unit です。

## 目的

- VM image と Live ISO をノード・プロバイダ間で共通化する。
- user-data には node identity と bootstrap pointer だけを置く。
- 完全な `router.yaml` または config bundle は HTTP / object storage から取得する。
- 取得した config はインストール前に検証する。
- 既存の `ROUTERD_CONFIG` config disk 方式を、オフライン・リムーバブルメディア向けの第一候補として維持する。
- transport secret や cloud credential を user-data に平文で置かない。

## User-data schema

routerd 固有の項目はトップレベルの `routerd` object に入れます。`hostname` は
cloud-init で一般的なトップレベル項目であり、routerd 起動前にも必要なのでそのまま使います。

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  node_role: onprem-router
  config_url: https://config.example.net/routerd/pve-rt07/bundle.tar.zst
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  transport_secret_ref: vault://routerd/pve-rt07/wireguard
```

| Field | Required | Meaning |
| --- | --- | --- |
| `hostname` | 推奨 | routerd 起動前に反映する node identity。 |
| `routerd.node_role` | 任意 | `onprem-router`、`spine`、`rr`、`edge` などの role hint。 |
| `routerd.config_url` | 任意 | 完全な routerd config または config bundle の URL。 |
| `routerd.config_sha256` | 信頼済みローカルネットワーク外で `config_url` を使う場合は必須 | 取得 object の SHA256 digest。 |
| `routerd.transport_secret_ref` | 任意 | Vault、cloud secret storage、operator 管理場所への pointer。secret 値そのものは user-data に置かない。 |

PR #546 の互換 alias (`config_url`、`config-url`、`configUrl`、`routerd_config_url`
および `config_sha256` の同種表記) は移行期間中に reader 側で受けてもよいですが、
新しい例では `routerd.*` 形を使います。

## Provider sources

bootstrap reader は provider 固有の data source を同じ user-data document に正規化します。

| Provider | Source | Notes |
| --- | --- | --- |
| PVE | `CIDATA` / `cidata` label の NoCloud config drive | まず `/user-data` を読み、OpenStack 形式 path を fallback にする。`qm set --cicustom user=...` で配布できる。 |
| AWS | IMDSv2 `http://169.254.169.254/latest/user-data` | user-data 取得前に session token を取る。 |
| Azure | IMDS `http://169.254.169.254/metadata/instance/compute/userData?...` | `Metadata: true` header を使い、返された user-data を base64 decode する。 |
| OCI | IMDSv2 `http://169.254.169.254/opc/v2/instance/metadata/user_data` | `Authorization: Bearer Oracle` header を使い、返された user-data を base64 decode する。 |

Live ISO の first implementation は軽量に保ちます。後で module 互換性が必要になるまでは、
完全な `cloud-init` package は入れません。Live ISO には systemd first boot path があるため、
小さな reader の方が ISO size と挙動を予測しやすくなります。

## 優先順位

boot 時の config discovery は次の順序にします。

1. 既存の `ROUTERD_CONFIG` config disk / USB media。
2. 現在の provider からの cloud-init user-data。
3. 組み込みの sample/default config。

Hostname は SSH identity や host-specific config disk path に必要なので、完全な config restore
より早く反映してよい項目です。NoCloud user-data の `hostname` は routerd service 起動前に
`/etc/hostname` へ書き込み、`hostnamectl set-hostname` を呼びます。

Live ISO の広い systemd-networkd DHCP profile は bootstrap 専用です。
`routerd.config_url` の取得には使えますが、first boot unit は config restore 後、
`routerd.service` 起動前に `/etc/systemd/network/80-dhcp.network` を削除し、
systemd-networkd を reload します。以後は `DHCPv4Client` や `IPv4StaticAddress`
などの routerd resource だけが、アドレスと route の管理元になる想定です。

config disk と cloud-init の両方が config URL を提供した場合、config disk を優先します。
ただし config disk が hostname を持たない場合、cloud-init 由来の hostname は使えます。

## Config bundle

取得 object は単一の `router.yaml` でも bundle archive でもよい形にします。bundle layout は
明示的かつ安定させます。

```text
router.yaml
secrets/
  README.txt
metadata.json
```

`metadata.json` には将来的に version、created time、intended node、signature metadata
を入れられます。最初の実装では、取得 object 全体の SHA256 check だけを必須にします。

失敗時の挙動:

- `config_sha256` があり一致しない場合、config をインストールしない。
- fetch に失敗し、既存 config もない場合、default config で続行し boot log に明示する。
- persistent storage に検証済みの過去 config がある場合、それを使い続ける。

## Security

- WireGuard key、provider credential、federation transport secret を user-data に直接置かない。
- user-data は node identity 情報として扱い、secret として扱わない。
- まず `config_sha256` で integrity を確認する。
- config bundle が multi-file release artifact になったり、信頼しないネットワークから取得される場合は signature verification を追加する。
- remote plugin registry と remote plugin install はスコープ外。

## 段階実装

1. 完了: Ubuntu debootstrap Live ISO で PVE NoCloud `hostname` を反映する。
2. PVE NoCloud では完了: user-data を parse し、任意の `routerd.config_sha256` 付きで `routerd.config_url` を取得する。
3. systemd first boot path では完了: config disk precedence、単一 `router.yaml` の配置、`.tar.zst` / `.tar.gz` / `.tar` bundle extraction。
4. 完了: AWS、Azure、OCI IMDS reader を同じ user-data parse interface の下に追加する。
5. 完了: Live ISO の SSH host key を再生成し、`ssh_authorized_keys` を配置し、sshd を有効化し、fetch 失敗時 fallback 用に最後の検証済み `router.yaml` を cache する。
6. 完了: config restore 後、`routerd.service` 起動前に bootstrap DHCP を削除し、OS DHCP route が競合する network manager として残らないようにする。
7. bundle format が固まったら signature verification と status reporting を強化する。

PR #546 から引き継ぐべき点は config pointer と checksum の考え方です。Alpine/OpenRC 固有の実装は
現在の Live ISO には持ち込まず、debootstrap ISO では systemd first boot flow に載せます。
