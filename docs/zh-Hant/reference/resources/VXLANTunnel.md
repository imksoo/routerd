# VXLANTunnel

[各資源的設定](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理具有靜態泛洪目的地及選用橋接的 Linux VXLAN 裝置，不設定多播探索或動態 MAC 學習。

## 指定條件與參照關係

- 操作控制器在非 Linux 系統直接返回，不套用設定。此資源應使用 Linux 目標，結構驗證通過不代表 FreeBSD 可運作。
- when 採關閉優先：false、未解析、過期或開機前的狀態會先停用再刪除裝置，不只是略過更新。每個參照狀態預設有效 30 秒，可用 maxAge 覆寫。驗證拒絕接受 unknown 的條件。
- tcpMSSClamp 需要 bridge 與明確指定的 mtu >=1280。產生規則時還需真正的 Bridge 資源及可解析的成員介面；全域參照驗證僅確認共用介面索引中的存在。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # 選填; string
    ifname: "<string>" # 選填; string
    localAddress: "<string>" # 必填; string
    mtu: 0 # 選填; integer; minimum=0
    outerDF: "" # 選填; string; enum=["", "inherit", "set", "unset"]
    peers: # 選填; array
      - "<string>" # string
    tcpMSSClamp: true # 選填; boolean
    udpPort: 1 # 選填; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # 必填; string
    vni: 1 # 必填; integer; minimum=1; maximum=16777215
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.ifname` | OS VXLAN 名稱，預設 metadata.name，最多 15 字元，不可含空白或斜線。 |
| `spec.vni` | 必填 VXLAN 網路識別碼，整數 1–16777215，須與目標遠端第二層網段一致。 |
| `spec.localAddress` | 必填的外層本機 IP 位址，不提供動態狀態來源。驗證只確認 IP 格式，不檢查本機指派或與對等端家族一致。 |
| `spec.peers` | 選用的遠端外層 IP 清單，預設為空。同步為 00:00:00:00:00:00 的 permanent self 泛洪 FDB 項目，移除多餘泛洪項目。空清單不提供靜態遠端泛洪目的地。 |
| `spec.underlayInterface` | 必填同一 Router 中宣告的邏輯介面，會解析為 OS 裝置名稱。無狀態來源或自動選擇。 |
| `spec.udpPort` | 外層 UDP 目的埠，省略或 0 預設 4789，明確範圍為 1–65535。 |
| `spec.mtu` | 選用的內層裝置 MTU 576–9216，省略或 0 使用 OS/既有 MTU。即使結構描述下限為 0，tcpMSSClamp 仍要求明確值 >=1280。 |
| `spec.bridge` | 選用的已宣告橋接名稱，解析為 OS 名稱並接為 VXLAN master，預設不連接。tcpMSSClamp 時必填，請使用 Bridge 資源而非其他介面類型。 |
| `spec.outerDF` | 外層 IPv4 DF 策略，預設 inherit，也可用 set/unset，傳入 ip link VXLAN df。不設定內層位址或路由。 |
| `spec.when` | 選用的共用 state/all/any 條件，另適用上述關閉優先及新鮮度規則。省略表示無條件；接受 unknown 的條件不可啟用此隧道。 |
| `spec.tcpMSSClamp` | 預設 false。true 為此 VXLAN 與 Bridge 成員埠之間建立 Linux bridge 家族 IPv4/IPv6 TCP SYN MSS 規則。取 VXLAN/橋接/成員的最小正 MTU，IPv4 減 40、IPv6 減 60，不處理任意路由流量。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)
