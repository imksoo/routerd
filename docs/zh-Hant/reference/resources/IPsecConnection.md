# IPsecConnection

[各資源的設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

產生 strongSwan swanctl 連線，並一併同步所有 routerd IPsec 連線。CHILD_SA 設為流量觸發的 trap；載入設定不代表 SA 已建立。

## 指定條件與參照關係

- preSharedKey 與 certificateRef 必須且只能指定一項。certificateRef 直接傳入 swanctl local.certs，不解析為 routerd 資源。
- 彙整設定位於 Linux 的 /etc/routerd/swanctl 或 FreeBSD 的 /usr/local/etc/routerd/swanctl。swanctl --load-all --file 同步整個管理集合與密鑰。憑證、信任錨點及配對私密金鑰須另行放入該設定使用的認證儲存區。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    [ certificateRef: <string> ]
    [ cloudProviderHint: ("" | "aws" | "azure" | "gcp") ]
    leftSubnet: <string>
    localAddress: <string>
    [ phase1Proposals:
        - <string>
    ]
    [ phase2Proposals:
        - <string>
    ]
    [ preSharedKey: <string> ]
    remoteAddress: <string>
    rightSubnet: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.localAddress` | **必填** | string | 必填的本機端點 IPv4/IPv6 位址，不接受主機名稱或介面參照。 |
| `spec.remoteAddress` | **必填** | string | 必填的遠端 IPv4/IPv6 位址。驗證不強制兩端與流量選擇器的位址家族一致。 |
| `spec.preSharedKey` | **條件必填** | string | 直接指定 IKE 共用密鑰；未使用 certificateRef 時必填。無預設值、檔案或環境來源，會寫入管理的 swanctl secrets 區段。 |
| `spec.certificateRef` | **條件必填** | string | strongSwan local.certs 使用的憑證名稱或路徑；未使用 preSharedKey 時必填，遠端認證設為 pubkey。不建立憑證或參照 routerd Secret。 |
| `spec.phase1Proposals` | *選填* | array | IKE 提案清單，預設 [aes256-sha256-modp2048]。非空清單直接以逗號連接，routerd 不驗證演算法支援。 |
| `spec.phase2Proposals` | *選填* | array | ESP 提案清單，預設 [aes256-sha256]，套用至名為 net 的子連線；演算法有效性由 strongSwan 載入時檢查。 |
| `spec.leftSubnet` | **必填** | string | 必填的本機流量選擇器 IPv4/IPv6 CIDR，輸出為 local_ts。 |
| `spec.rightSubnet` | **必填** | string | 必填的遠端流量選擇器 IPv4/IPv6 CIDR，輸出為 remote_ts。 |
| `spec.cloudProviderHint` | *選填* | string; enum=["", "aws", "azure", "gcp"] | 選用的 aws、azure 或 gcp 標籤，預設為空。僅供觀察回報，不變更提案、認證或 swanctl 產生內容，不建立雲端資源。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`
