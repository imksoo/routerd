# IPsecConnection

[各資源的設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

產生 strongSwan swanctl 連線，並一併同步所有 routerd IPsec 連線。CHILD_SA 設為流量觸發的 trap；載入設定不代表 SA 已建立。

## 指定條件與參照關係

- preSharedKey 與 certificateRef 必須且只能指定一項。certificateRef 直接傳入 swanctl local.certs，不解析為 routerd 資源。
- 彙整設定位於 Linux 的 /etc/routerd/swanctl 或 FreeBSD 的 /usr/local/etc/routerd/swanctl。swanctl --load-all --file 同步整個管理集合與密鑰。憑證、信任錨點及配對私密金鑰須另行放入該設定使用的認證儲存區。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 選填; string
    cloudProviderHint: "" # 選填; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必填; string
    localAddress: "<string>" # 必填; string
    phase1Proposals: # 選填; array
      - "<string>" # string
    phase2Proposals: # 選填; array
      - "<string>" # string
    preSharedKey: "<string>" # 選填; string
    remoteAddress: "<string>" # 必填; string
    rightSubnet: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.localAddress` | 必填的本機端點 IPv4/IPv6 位址，不接受主機名稱或介面參照。 |
| `spec.remoteAddress` | 必填的遠端 IPv4/IPv6 位址。驗證不強制兩端與流量選擇器的位址家族一致。 |
| `spec.preSharedKey` | 直接指定 IKE 共用密鑰；未使用 certificateRef 時必填。無預設值、檔案或環境來源，會寫入管理的 swanctl secrets 區段。 |
| `spec.certificateRef` | strongSwan local.certs 使用的憑證名稱或路徑；未使用 preSharedKey 時必填，遠端認證設為 pubkey。不建立憑證或參照 routerd Secret。 |
| `spec.phase1Proposals` | IKE 提案清單，預設 [aes256-sha256-modp2048]。非空清單直接以逗號連接，routerd 不驗證演算法支援。 |
| `spec.phase2Proposals` | ESP 提案清單，預設 [aes256-sha256]，套用至名為 net 的子連線；演算法有效性由 strongSwan 載入時檢查。 |
| `spec.leftSubnet` | 必填的本機流量選擇器 IPv4/IPv6 CIDR，輸出為 local_ts。 |
| `spec.rightSubnet` | 必填的遠端流量選擇器 IPv4/IPv6 CIDR，輸出為 remote_ts。 |
| `spec.cloudProviderHint` | 選用的 aws、azure 或 gcp 標籤，預設為空。僅供觀察回報，不變更提案、認證或 swanctl 產生內容，不建立雲端資源。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`
