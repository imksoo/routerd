# SAM E2E Test Infrastructure

routerd Selective Address Mobility (SAM) の E2E テスト基盤。
AWS/Azure/OCI/PVE にまたがるマルチクラウド環境を OpenTofu で構築し、
pseudo-client 間の SSH hostname verification を合否判定の正本とする。

## Quick start

### 1. Provider 認証の設定

各プロバイダの CLI 認証を一度だけ設定する:

```bash
# AWS
aws configure --profile <name>

# Azure
az login

# OCI
oci setup config       # ~/.oci/config に profile を作成

# PVE (API token を環境変数に設定)
export TF_VAR_pve_api_token='root@pam!tofu=<secret>'
```

### 2. tfvars の作成

```bash
cd tests/e2e/sam/terraform/envs/default
cp terraform.tfvars.example terraform.tfvars
# terraform.tfvars を編集して実際の値を記入
```

`topology_scale` は実機コストと試験段階に合わせて選ぶ:

- `single`: 最初の apply 確認用。AWS RR 1台と、AWS/Azure/OCI/PVE
  各siteの leaf/client 1組だけを作る。
- `full`: 最終matrix用。AWS RR 2台と、AWS/Azure/OCI/PVE
  各siteの leaf/client 2組を作る。

### 3. インフラ構築

```bash
../../scripts/sam-preflight.sh \
  --tfvars terraform.tfvars \
  --evidence-dir /tmp/sam-e2e-preflight
tofu init
tofu plan
tofu apply
tofu output -json > tofu-output.json
```

### 4. routerd config の生成

```bash
../../configs/sam-e2e-generate.sh \
  --tofu-output tofu-output.json \
  --out-dir /tmp/sam-e2e-configs
```

### 5. E2E テストの実行

```bash
../../scripts/sam-e2e.sh \
  --tofu-output tofu-output.json \
  --artifact /path/to/routerd-<version>-linux-amd64.tar.gz \
  --evidence-dir /tmp/sam-e2e-evidence
```

### 6. 環境の破棄

```bash
tofu destroy
```

## Standard topology

`topology_scale = "full"` の標準トポロジ:

```
  AWS VPC 10.77.0.0/16
  ├── RR subnet 10.77.10.0/24
  │   ├── aws-rr-a    (10.77.10.10)
  │   └── aws-rr-b    (10.77.10.11)
  └── Leaf subnet 10.77.60.0/24
      ├── aws-leaf-a   (10.77.60.4)  + aws-client-a   (10.77.60.11)
      └── aws-leaf-b   (10.77.60.5)  + aws-client-b   (10.77.60.16)

  Azure VNet 10.77.60.0/24
      ├── azure-leaf-a (10.77.60.14) + azure-client-a (10.77.60.12)
      └── azure-leaf-b (10.77.60.21) + azure-client-b (10.77.60.17)

  OCI VCN 10.77.60.0/24
      ├── oci-leaf-a   (10.77.60.24) + oci-client-a   (10.77.60.13)
      └── oci-leaf-b   (10.77.60.25) + oci-client-b   (10.77.60.18)

  PVE (on-prem)
      ├── pve-leaf-a   (10.77.60.34) + pve-client-a   (10.77.60.15)
      └── pve-leaf-b   (10.77.60.35) + pve-client-b   (10.77.60.19)
```

## Output schema

`tofu output -json` は `nodes` と `fabric` を出力する。

**nodes** -- 全ノードの接続情報:
```json
{
  "node-name": {
    "name": "...",
    "role": "rr|leaf|client",
    "site": "aws|azure|oci|pve",
    "ssh_user": "ubuntu",
    "instance_id": "...",
    "interface_id": "...",
    "private_ip": "10.77.x.x",
    "public_ip": "x.x.x.x",
    "overlay_ip": "10.99.0.x"
  }
}
```

**fabric** -- トポロジーメタデータ (mobility prefix, BGP ASN, provider 別リソース ID)。

`sam-e2e-generate.sh` と `sam-e2e.sh` はこの出力のみを入力として動作する。

## Test options

```
sam-e2e.sh [options]
  --failover-node NODE       指定ノードの routerd を停止して failover テスト
  --rejoin-after-failover    停止した failover node を再起動して rejoin テスト
  --load-balance-report      owner table のスナップショットを取得
  --skip-matrix              SSH hostname matrix をスキップ
  --skip-legacy-protocols    FTP/RPC/NFS/CIFS テストをスキップ
  --performance-tests        SAM経由のiperf3/ping性能テストと、AWS/Azure/OCIのcloud間public直結比較を実行
  --failover-transfer-tests  failover停止操作中にclient間HTTP転送を流して結果を保存
  --destroy-cmd CMD          テスト後に実行する破棄コマンド
```

`--performance-tests` のSAM経由性能テストは全疑似クライアント間を対象にする。
比較用のpublic直結iperf3/pingはcloud間のみを対象にし、AWS/Azure/OCIの異なる
cloud pairだけを実行する。同一cloud内、およびPVE/on-premを含むpairは
`public-summary.tsv` に skip として記録する。

## Full validation sequence

実機検証の前に `~/routerd-orchestration.md` と
`cloudedge-mobility/LAB_POLICY.md` を読み直し、run note に記録する。
OCI の `compartment_ocid` は display name が `ManagedCompartmentForPaaS`
ではないことを apply 前に確認し、その出力を evidence に保存する。

標準の進め方:

```bash
cd tests/e2e/sam/terraform/envs/default
../../scripts/sam-preflight.sh --tfvars terraform.tfvars --evidence-dir /tmp/sam-preflight
tofu plan
tofu apply
tofu output -json > tofu-output.json
```

1. `topology_scale = "single"` で apply と初回 E2E を確認する。
2. `topology_scale = "full"` に変更し、2RR/8leaf/8client を apply する。
3. baseline は `--load-balance-report --performance-tests` 付きで実行し、
   full SSH hostname matrix、legacy protocol、SAM private performance、
   AWS/Azure/OCI cloud 間 public direct comparison を保存する。
4. RR redundancy は `--skip-deploy --failover-node aws-rr-a
   --rejoin-after-failover --failover-transfer-tests` と `aws-rr-b` で別々に実行する。
5. Leaf failover は各siteのA/B両系、例: `aws-leaf-a` / `aws-leaf-b`、
   `azure-leaf-a` / `azure-leaf-b`、`oci-leaf-a` / `oci-leaf-b`、
   `pve-leaf-a` / `pve-leaf-b` を対象にし、
   `--rejoin-after-failover --load-balance-report --performance-tests
   --failover-transfer-tests` を付ける。
6. Load-balance は `--skip-deploy --load-balance-report` で owner table、
   route、traceroute、E2E matrix を同じ evidence directory に残す。
7. 成功時だけ `tofu destroy` し、provider/PVE inventory で残存がないことを
   cleanup evidence に保存する。失敗時は destroy せず live inspection を行う。
8. run完了後、`scripts/sam-e2e-summary.sh <evidence-dir>` で matrix/legacy/
   performance/failover-transfer/provider evidence の要約を作り、PR/issue
   コメントには要約と raw evidence path の両方を残す。

フル構成を apply 済みの場合は、標準scenarioをまとめて実行できる:

```bash
../../scripts/sam-full-validation.sh \
  --tofu-output tofu-output.json \
  --artifact /path/to/routerd-<version>-linux-amd64.tar.gz \
  --evidence-root /tmp/sam-full-validation
```

`sam-full-validation.sh` は途中scenarioが失敗したらそこで停止し、
`--destroy-cmd` が指定されていても実行しない。失敗時は live environment を
残したまま、該当scenarioの evidence と実機状態を確認してから次の判断を行う。
全scenario成功後に `--destroy-cmd` を実行した場合は、
`sam-post-destroy-inventory.sh` で provider/PVE の残存確認を行い、
`post-destroy/` と `post-destroy-summary.txt` にcleanup evidenceを保存する。
`--list-scenarios` を付けると、`tofu output` に必要nodeが存在することだけを
確認し、実機へSSHせずに実行予定scenarioを表示する。
失敗scenarioの再試行や段階実行では `--scenario <name>` を指定できる。
`--scenario` は複数回指定できるが、部分実行時は誤cleanupを避けるため
`--destroy-cmd` を指定できない。

`sam-e2e.sh` は各 convergence run の所要秒数を
`convergence/summary.tsv` に記録する。failover/rejoin では
`diagnostics/before-*` と `diagnostics/after-*` に doctor/status/owner table、
OS route/address、routerd/routerd-bgp journal を保存する。
`provider/` には `tofu output` の fabric/nodes と、利用可能なCLIから取得した
AWS instance/ENI/route table、Azure VM/NIC/route table/RBAC、OCI compartment/
instance/VNIC/route table、PVE VMID/bridge情報を保存する。これらはE2E判定の
補助証跡であり、PASS/FAILの正本は疑似クライアント間E2Eのままとする。
`--failover-transfer-tests` は停止操作の直前に疑似クライアント間の
SAM private HTTP 転送を開始し、`failover-transfer/` に転送ペア、curl結果、
終了コード、受信ファイルサイズを保存する。

## GitHub Actions (将来)

認証情報を GitHub Secrets に設定することで CI からも実行可能:

| Secret | 内容 |
|--------|------|
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | AWS IAM |
| `AZURE_SUBSCRIPTION_ID` + OIDC | Azure |
| `OCI_CONFIG_FILE` | OCI API key |
| `PVE_API_TOKEN` | Proxmox VE |

E2E テストは `workflow_dispatch` トリガーのオプション実行とし、
通常の CI/CD パイプラインの required check には含めない。

## Files

```
tests/e2e/sam/
├── .gitignore                  # *.tfvars, *.tfstate, .terraform/
├── README.md                   # このファイル
├── configs/
│   └── sam-e2e-generate.sh     # tofu output → routerd YAML config
├── scripts/
│   ├── sam-e2e.sh              # E2E test harness
│   ├── sam-full-validation.sh  # Standard full-topology scenario runner
│   ├── sam-e2e-summary.sh      # Evidence summary helper
│   ├── sam-post-destroy-inventory.sh # Post-destroy cleanup evidence
│   └── sam-preflight.sh        # Pre-apply provider checks
└── terraform/
    ├── modules/
    │   ├── aws_rr/             # AWS Route Reflector (VPC, SG, IAM, EC2)
    │   ├── aws_leaf/           # AWS Leaf (Subnet, EC2 router+client)
    │   ├── azure_leaf/         # Azure Leaf (RG, VNet, NSG, VM)
    │   ├── oci_leaf/           # OCI Leaf (VCN, Security List, Instance)
    │   └── pve_leaf/           # Proxmox VE Leaf (VM, capture bridge)
    └── envs/
        └── default/            # Standard 2RR + 8leaf + 8client
            ├── main.tf
            ├── outputs.tf
            ├── variables.tf
            ├── versions.tf
            ├── providers.tf
            └── terraform.tfvars.example
```
