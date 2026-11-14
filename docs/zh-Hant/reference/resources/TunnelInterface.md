# TunnelInterface

[各資源的設定](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

建立並調和未加密的 IPv4 外層隧道及選用內層 IPv4 位址，metadata.name 為實際 OS 隧道名稱。

## 指定條件與參照關係

- 兩端點各須固定值或共用狀態來源其中之一，僅限 IPv4。動態端點缺值即使 optional true 仍暫停調和，不自動撤回既有轉送；無 when/dependsOn。
- Linux 以 ip link/address 及 FOU 接聽支援四模式。FreeBSD 僅 IPIP/gifN、GRE/greN，拒絕明確 TTL，address 非空時須明確內層 peerAddress。控制器強制鏈路 up，無 managed/enabled 停用選項。
- 設定 address 取代隧道其他 IPv4，空值全移除。Linux 清理亦移除未設定 IPIP/GRE（內建及其他設定保留者除外）及不用的 FOU 接聽；FreeBSD 撤除依記錄狀態及類型檢查。變更依整體設定，不以所有權限制。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ encapDport: <integer> ]
    [ encapSport: <integer> ]
    [ key: <integer> ]
    [ local: <string> ]
    [ localFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    mode: ("ipip" | "gre" | "fou" | "gue")
    [ mtu: <integer> ]
    [ pathMTU:
        [ forceFragmentIPv4: <boolean> ]
    ]
    [ peerAddress: <string> ]
    [ remote: <string> ]
    [ remoteFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    trustedUnderlay: <boolean>
    [ ttl: <integer> ]
    [ underlayInterface: <string> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.mode` | **必填** | string; enum=["ipip", "gre", "fou", "gue"] | 必填 ipip/gre/fou/gue，無預設。Linux FOU/GUE 使用 UDP 封裝 IPIP 鏈路；FreeBSD 僅支援 ipip（gifN）及 gre（greN），拒絕 FOU/GUE。 |
| `spec.local` | **條件必填** | string | 本機外層純 IPv4 端點，local/localFrom 恰需一個。拒絕固定 CIDR、主機名稱與 IPv6，不自動選擇介面位址。 |
| `spec.localFrom` | **條件必填** | object | 選用共用狀態來源，須明確指定既有 Kind/name 與提供欄位，與 local 互斥。執行時僅讀狀態，接受單一 IPv4 或 CIDR 並正規化；即使 optional true，缺值仍為 Pending/EndpointSourcePending，無端點替代值。 |
| `spec.remote` | **條件必填** | string | 遠端外層純 IPv4 端點，remote/remoteFrom 恰需一個，拒絕固定主機名稱、IPv6 與 CIDR。 |
| `spec.remoteFrom` | **條件必填** | object | 選用共用狀態來源，與 remote 互斥；必要 field、IPv4/CIDR 正規化及忽略 optional 行為同 localFrom。狀態缺值暫停調和，但不自動拆除既有隧道。 |
| `spec.address` | **條件必填** | string | 選用內層 IPv4 主機/CIDR。控制器移除隧道其他 IPv4，省略或空值因此亦移除既有 IPv4；無自動內層位址。FreeBSD 設定後需 peerAddress。 |
| `spec.peerAddress` | **條件必填** | string | 僅 FreeBSD 的明確內層純 IPv4 對端，與 address 必須成對，不由 CIDR 推導，Linux 拒絕。 |
| `spec.underlayInterface` | *選填* | string | 選用 MTU 估算資源名稱（Interface、WireGuardInterface、巢狀 TunnelInterface）。此處非必填且未全域驗證存在，未知使用 1500 替代。目前隧道命令不綁定此裝置，外層出口由主機選路，請避免遞迴參照。 |
| `spec.mtu` | *選填* | integer; minimum=576; maximum=9216 | 選用明確 576–9216，原樣使用。省略或 0 為設定底層 MTU（替代 1500）減 ipip 20、gre 24（有 key 加 4）、fou 28、gue 32。替代預設 1480/1476/1472/1468，有 key GRE 1472，非即時 PMTU 探索。 |
| `spec.ttl` | *選填* | integer; minimum=1; maximum=255 | Linux 外層 TTL 1–255，省略或 0 為 64。FreeBSD 拒絕非零明確 TTL，ifconfig 路徑亦不套用內部回報預設 64。 |
| `spec.key` | *選填* | integer; minimum=0; maximum=4294967295 | 僅 GRE 的整數 0–4294967295，預設 0 表示無 key；其他模式非零拒絕。FreeBSD 使用 ifconfig grekey，Linux key/nokey。它是識別碼，不是加密或認證秘密。 |
| `spec.encapSport` | **條件必填** | integer; minimum=1; maximum=65535 | fou/gue 必填 UDP 來源及本機接收連接埠 1–65535，無預設，控制器亦在此建立 FOU/GUE 接聽；ipip/gre 非零拒絕。 |
| `spec.encapDport` | **條件必填** | integer; minimum=1; maximum=65535 | fou/gue 必填遠端 UDP 目的連接埠 1–65535，須符合對端接收埠，無預設；ipip/gre 非零拒絕。 |
| `spec.trustedUnderlay` | **必填** | boolean | 必須 true，false 或省略會拒絕。IPIP/GRE/FOU/GUE 不提供加密或對端認證，此確認亦不增加這些功能。 |
| `spec.pathMTU` | *選填* | object | 選用轉送路徑策略選項，預設不強制 IPv4 分片，獨立於明確隧道 MTU。 |
| `spec.pathMTU.forceFragmentIPv4` | *選填* | boolean | 預設 false，Linux 路徑 MTU 策略可清除符合衍生轉送路徑之過大 IPv4 的 DF，不是隧道 ignore-df 選項或 IPv6 功能。FreeBSD 略過此 nftables 控制器，此路徑無同等動作。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/tunnel.go) · [internal/ipiptunnel/ipip.go](https://github.com/imksoo/routerd/blob/main/internal/ipiptunnel/ipip.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `mode`, `local`, `remote`, `peerAddress`, `observedAddress`, `observedPeerAddress`, `underlayInterface`, `underlayMTU`, `tunnelOverhead`, `mtu`, `ttl`, `encapSport`, `encapDport`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
