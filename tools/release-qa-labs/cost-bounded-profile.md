# Cost-bounded Cloud SAM profile / コストを抑えた Cloud SAM 試験構成

## English

The current `representative-redundancy` release-QA profile keeps ten routers:
two leaves at each of AWS, Azure, OCI, and PVE, plus two separate PVE RRs on
distinct hosts. Each site has exactly one client, for 14 guests total. A
client is not dedicated to leaf A or B: the same endpoint participates in
baseline, RR-A continuity, and each independent edge-A stop/rejoin scenario.
All 12 directed cross-site client pairs and all 9 cloud-ingress pairs remain
mandatory at baseline and both sides of each edge transition. All surviving
leaf control, dataplane, provider, and ownership gates remain required; RR
canaries never substitute for the edge matrices. Same-site client-to-client
coverage and B-side fault injection are not claimed.

The closed Terraform input explicitly sets `clients_per_site = 1` exactly
once. The plan guard rejects the former two-client shape, missing nodes, extra
nodes, and unexpected resources. Cloud admission requires 38 managed resources
and exactly three declared OCI VNIC-attachment reads. PVE requires five
workloads plus one separate disposable template stage; all six VMIDs are
recovered after testing. The seven inventory *scopes* are unchanged.

| Guest role | Count | Exact size |
| --- | ---: | --- |
| AWS leaves | 2 | `t3.small` |
| AWS client | 1 | `t3.micro` |
| Azure leaves/client | 3 | `Standard_B1s` |
| OCI leaves/client | 3 | `VM.Standard.E4.Flex`, explicit 1 OCPU and 1 GB |
| PVE leaves/client/RRs | 5 | 1 core × 1 socket, 1024 MiB |

OCI Flex defaults are not acceptable substitutes: the saved plan must have
one explicit `shape_config` with `ocpus = 1` and `memory_in_gbs = 1` for every
OCI guest. Missing/unknown fields or implicit 16 GB memory fail admission.
The stopped PVE template stage is not a sixth running workload.

The review rates are USD 0.04/h per AWS small, 0.02/h per AWS micro, 0.03/h per
Azure B1s, and 0.04/h per OCI 1-OCPU/1-GB Flex. Over the unchanged 8700-second
paid cleanup envelope, plus USD 0.30 storage/IPv4 allowance, the policy estimate
is approximately USD 1.05. The admission ceiling remains USD 1.60. These are
policy inputs, not a provider price quote or an actual-bill cap; data transfer,
minimum storage billing, credits, and delayed cleanup can affect the bill.

Source policy is independent of execution location. The default
`execution.sourcePolicy: canonical-remote` requires the published exact RC.
An authorized unpublished local build uses `local-pinned` only together with
`hostPolicy: local-supervised`. It retains exact clean HEAD, canonical origin,
untagged/frozen-main provenance, all script and artifact hashes, immutable
checkout and supervisor pins. Only remote RC ref/advertisement requirements
are omitted. Results explicitly say `local-test-only`; no push, main merge,
tag, public RC publication, or Release is implied.

The historical cloud-plan fixture is retained unchanged. The new one-client
and PVE plan fixtures are derived offline contracts, not evidence of a live
apply or functional PASS. Actual saved plans and live test outcomes remain
separate evidence. Run cleanup after success, failure, timeout, or disconnect;
do not retain deallocated test VMs, disks, IPs, or networks by default.

Certification validates the embedded run schema before invoking its provider
driver. That schema accepts the current five-node PVE VMID map while retaining
optional client B for reading historical six-node manifests; it does not alter
the live guard's exact five-node requirement. The local `sourcePolicy` remains
embedded in the resulting certification manifest.

## 日本語

現行構成は AWS/Azure/OCI/PVE の edge 2 台ずつと、独立ホスト上の PVE RR 2 台、
計 10 router を維持する。client は各環境 1 台、合計 14 guest とする。
client は A/B 専属ではなく、同じ endpoint で平常・RR-A 障害・各 edge-A の
停止/復帰を確認する。環境間 12 directed E2E と 9 cloud-ingress、および
surviving leaf の control/dataplane/provider/ownership gate は省略しない。
同一環境内の client 間試験と B 系障害の実施を主張しない。

`clients_per_site = 1` を tfvars に一度だけ明記する。cloud は 38 managed と
3 OCI data read、PVE は 5 workload と停止状態の disposable stage 1 台を
exact 検査する。上表のサイズを固定し、特に OCI Flex の暗黙 16 GB を拒否する。
PVE の 6 VMID は全回収対象であり、zero 確認の 7 scope とは別の数である。

既存の 8700 秒 paid cleanup 枠を維持し、保守 policy estimate は約 1.05 USD、
admission ceiling は 1.60 USD のままとする。実際の請求額上限ではない。
旧 8 client / 56 / 42 の記録や fixture は過去の証跡として保持し、書き換えない。

未公開のローカルビルド試験は `local-supervised` と `sourcePolicy: local-pinned`
を明示し、公開済み RC の確認だけを省略する。clean exact HEAD、canonical origin、
untagged/frozen main、script/artifact hash、root-owned checkout、supervisor pins
は維持する。結果は `local-test-only` と記録し、push/merge/tag/Release を意味しない。
成功・失敗に関わらず試験用 VM/disk/IP/network を削除し、停止保持を既定に戻さない。

certification は provider driver の実行前に embedded run schema を確認する。
現行 5 VMID を受理し、旧 6 VMID の証跡読込用に client-B だけを optional とする。
現行 live guard の 5 名限定は緩めず、local sourcePolicy も manifest 内に保持する。
