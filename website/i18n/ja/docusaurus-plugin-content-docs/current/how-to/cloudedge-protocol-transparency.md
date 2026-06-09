---
title: CloudEdge プロトコル透過性の受け入れ検証
---

# CloudEdge プロトコル透過性の受け入れ検証

![CloudEdge プロトコル透過性プローブの FTP、NFS、バルク転送、PMTU、ソース IP 保持、no-NAT エビデンスの流れ](/img/diagrams/how-to-cloudedge-protocol-transparency.png)

CloudEdge mobility が、NAT、ヘルパー ALG、動的ポート、MTU/PMTU の振る舞いに敏感なコネクション指向プロトコルに対して透過的であることを検証するための、クラウドを使わないハーネス計画です。実際のライブランはラボ運用者が後日実施します。このドキュメントと `scripts/` 配下のスクリプトは、契約とエビデンスの形式を準備するだけです。

## 目標

論理共有サブネット（デモでは `10.77.60.0/24`）上のトラフィックについて、以下を証明します:

- NAT なし: サーバーがクライアントサイトの mobility `/32` をピアアドレスとして認識する。
- クライアントのデフォルトゲートウェイがローカルサイトから変更されていない。
- FTP アクティブモードとパッシブモードの両方で、NAT ALG なしにデータ転送が完了する。
- `rpcbind` 経由の RPC エンドポイント探索と、NFSv3 のマウント/読み書きがサイト間で動作する。
- 大容量転送が PMTU ブラックホールなしに完了する。
- MSS/PMTU エビデンスがオーバーレイ MTU、ルート MTU/advmss（利用可能な場合）、設定済み MSS clamp 値を記録する。

## 最小限のライブマトリクス

通常の D3 方向付きマトリクスがすでにグリーンになった後で、プロトコルプローブを実行します。代表的な 2 ペアを使用します:

| ペア | 理由 |
| --- | --- |
| `aws -> azure` | 両端でクラウドプロバイダートラッピングを行うクラウド間オーバーレイパス |
| `aws -> onprem` | on-prem 側で proxy-ARP/VRRP 権限を持つクラウド-on-prem 間パス |

シナリオカタログでは、これを `examples/cloudedge-acceptance-scenarios.json` の `d11-protocol-transparency` としてエンコードしています。

パリティ検証を拡大する場合は `azure -> oci`、`oci -> aws`、逆方向などを追加できますが、最小限の受け入れ検証は 1 回の 4 サイトラボウィンドウ内で実行できる規模に収めるべきです。

## ハーネス

ラッパーは以下の通りです:

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
  scripts/cloudedge-protocol-probe.sh \
    --pairs aws:azure,aws:onprem \
    --bytes 104857600 \
    --out evidence/protocol-probe.json
```

完全な受け入れシナリオは同じラッパーを以下のように使用します:

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
MATRIX_RUNNER=scripts/runners/cloudedge-matrix-runner.sh \
scripts/cloudedge-acceptance.sh run \
  --scenario d11-protocol-transparency \
  --out evidence/d11-protocol \
  --commit <routerd-commit>
```

出力は `scripts/cloudedge-protocol-result-schema.json` で検証され、`result.json` の `protocols` オブジェクト配下に取り込まれます。

## ランナーコントラクト

`scripts/runners/cloudedge-protocol-runner.sh` は `PROTOCOL_PROBE_RUNNER` を実装します。意図的に環境変数でパラメータ化されており、プロバイダーアカウント ID、リソース ID、秘密情報は含まれていません。

サイトごとに必要な設定:

```sh
export CE_AWS_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AWS_CLIENT_IP=10.77.60.11
export CE_AZURE_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AZURE_CLIENT_IP=10.77.60.12
export CE_ONPREM_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export ONPREM_CLIENT_IP=10.77.60.10
export SSH_KEY_FILE=<private-key>
export SSH_USER=ubuntu
export CLIENT_SSH_USER=ubuntu
```

プロトコル関連の変数:

```sh
export CE_PROTOCOL_INSTALL=1
export CE_PROTOCOL_CONFIGURE_SERVICES=1
export CE_PROTOCOL_FTP_PASSIVE_PORTS=40000:40100
export CE_PROTOCOL_BULK_BYTES=104857600
export CE_PROTOCOL_PMTU_SIZE=1300
export CE_PROTOCOL_OVERLAY_IFACE=wg-hybrid
export CE_PROTOCOL_MSS_CLAMP=1340
```

各オペレーションはランナーを編集せずにオーバーライドできます:

```sh
export CE_PROTOCOL_FTP_ACTIVE_COMMAND='...'
export CE_PROTOCOL_NFS_COMMAND='...'
```

ラッパーはペアごとに以下のオペレーションを呼び出します:

| オペレーション | アサーション |
| --- | --- |
| `setup` | 有効時に `vsftpd`、`rpcbind`、NFS サーバー/クライアントツール、`iperf3` をインストール/設定 |
| `ftp-active` | FTP `PORT` モードデータチャネルが完了 |
| `ftp-passive` | FTP パッシブモードデータチャネルが完了 |
| `nfs` | NFSv3 マウント + 要求バイト量の書き込み/読み取りが完了 |
| `rpc` | `rpcinfo -p` が `rpcbind` と少なくとも 1 つの動的 RPC/NFS ポートを検出 |
| `bulk` | `iperf3 -n <bytes>` が完了し、スループット/再送を記録 |
| `pmtu` | DF ping が成功し、オーバーレイ MTU、ルート MTU/advmss、MSS clamp を記録 |
| `source-preserved` | サーバー側 SSH がクライアントの mobility `/32` をピア IP として認識 |
| `no-nat` | 同じピア IP チェックを、明示的な no-NAT アサーションとして記録 |

## Forcefrag / MSS の比較

通常の実行では routerd 導出の MSS clamp で問題なくパスするはずです。ラボで P2-b の force fragmentation 動作を証明する必要がある場合は、同じ D11 ペアセットを 2 回実行します:

1. `forceFragmentIPv4: false`（デフォルト）: TCP 転送は MSS clamp で通過するはず。オーバーサイズの DF 非 TCP はアンダーレイの PMTU に依存して失敗する場合があります。
2. 該当する `OverlayPeer` または `TunnelInterface` で `forceFragmentIPv4: true`: 同じ DF プローブがパスし、ルーターエビデンスに `routerd_forcefrag` が表示されるはずです。

force fragmentation をグローバルに有効化しないでください。パススコープに限定し、before/after の設定ダイジェストをエビデンスバンドルに記録してください。

## エビデンスレビューチェックリスト

`protocol-probe.json` の各ペアについて:

- `checks.ftpActive`、`ftpPassive`、`nfs`、`rpc`、`bulkTransfer`、`pmtu`、`sourceIpPreserved`、`noNat` がすべて `pass`。
- `details.sourceIpPreserved.peer_ip` がクライアントサイトの mobility `/32` と一致。
- `details.rpc.dynamic_port` が存在し、`111` ではない。
- `iperf3` が利用可能な場合、`details.bulkTransfer.retransmits` が記録されている。
- `details.pmtu.overlay_mtu`、`route_mtu` または `route_advmss`、`mss_clamp` が記録されている。

外側の `result.json` には以下のパスアサーションを含める必要があります:

- `protocol_transparency`
- `ftp_active_passive`
- `nfs_rpc`
- `bulk_transfer_pmtu`
- `protocol_source_ip_preserved`
- `protocol_no_nat`
