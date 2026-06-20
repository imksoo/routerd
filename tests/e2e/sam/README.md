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
  --load-balance-report      owner table のスナップショットを取得
  --skip-legacy-protocols    FTP/RPC/NFS/CIFS テストをスキップ
  --performance-tests        iperf3/ping 性能テストを実行
  --destroy-cmd CMD          テスト後に実行する破棄コマンド
```

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
│   └── sam-e2e.sh              # E2E test harness
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
