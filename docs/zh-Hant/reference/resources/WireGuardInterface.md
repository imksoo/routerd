# WireGuardInterface

[各資源的設定](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

建立或設定 WireGuard 裝置，透過 wg setconf 套用完整對等端設定。位址指派與一般路由由其他資源設定。

## 指定條件與參照關係

- 執行時需提供 privateKey 或 privateKeyFile，否則維持 Pending/PrivateKeyMissing。兩者並非互斥：優先使用直接指定的鍵，但套用時仍會檢查或建立 privateKeyFile。
- 靜態 WireGuardPeer.interface 參照此資源的 metadata.name，而非 ifname。匯入的對等端依 Kind/name 合併；較後來源覆蓋較早來源，明確宣告的同名 WireGuardPeer 覆蓋產生的對等端。
- 使用註冊式 direct SAMPeerGroup 時，必須放在 peersFrom 最後，前方須有非 optional 的 SAMRRSet。執行時也需相同註冊原則的可用 RR 對等端；direct 不可用或不相容時仍保留 RR 連線。
- listenPort 會保留該 UDP 埠，不可與其他設定的監聽衝突。Linux 在指定正數埠時也會加入 INPUT UDP 允許規則。Up 表示介面已設定，不保證對等端交握成功。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    [ ifname: <string> ]
    [ listenPort: <integer> ]
    [ mtu: <integer> ]
    [ peersFrom:
        -
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ privateKey: <string> ]
    [ privateKeyFile: <string> ]
    [ selfNodeRef: <string> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.ifname` | *選填* | string | OS 裝置名稱，預設 metadata.name。目前即使指定 ifname，驗證仍檢查 metadata.name 的空白、斜線及 15 字元上限，未對 ifname 套用相同檢查。 |
| `spec.selfNodeRef` | *選填* | string | 匯入時排除的本機 SAM 節點識別，預設 Router.metadata.name。這是 nodeRef 值，不是 Kind/name 狀態參照。 |
| `spec.privateKey` | **條件必填** | string | 直接指定 WireGuard 私密金鑰，預設為空，優先於 privateKeyFile。使用 WireGuard Base64 金鑰格式；設定驗證不保證金鑰有效，套用時 wg 或檔案金鑰檢查仍可能失敗。 |
| `spec.privateKeyFile` | **條件必填** | string | 路由器上的選用金鑰檔案路徑。實際套用時，缺少的檔案以 0600 建立，新父目錄為 0700。既有檔案須含有效的 32 位元組 Base64 私密金鑰；空檔或無法讀取會失敗，不會覆寫。不是環境變數檔。 |
| `spec.listenPort` | *選填* | integer; minimum=1; maximum=65535 | 選用 UDP 監聽埠 1–65535。省略或 0 時不輸出 ListenPort，由 WireGuard 選擇或保留埠；routerd 不為 0 新增明確的埠例外。 |
| `spec.mtu` | *選填* | integer; minimum=576; maximum=9216 | 選用 MTU 576–9216。省略或 0 保留既有值或 OS 建立時預設值；此處不執行 wg-quick 的 MTU 計算。 |
| `spec.peersFrom` | *選填* | array | 選用且有順序的 SAM 對等端來源清單，預設為空。非 optional 來源缺少時介面維持 Pending/PeersFromPending，靜態對等端不會略過此條件。已解析但為空的清單不同於來源缺少。 |
| `spec.peersFrom[].resource` | **條件必填** | string | 必填來源，格式為 SAMNodeSet/name、SAMPeerGroup/name、SAMEnrollmentPolicy/name 或 SAMRRSet/name，不使用 field。此處驗證語法，解析對等端時查找來源。會略過自己及沒有 WireGuard 資料的節點。 |
| `spec.peersFrom[].optional` | *選填* | boolean | 預設 false。true 允許來源缺少而不使介面待處理，但不會全面忽略格式錯誤的來源。註冊式 direct 群組另適用上述保留 RR 的規則。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `publicKey`, `selfNodeRef`, `listenPort`, `fwmark`, `peerCount`, `hostFirewall`, `peersFrom`, `pendingSources`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
