# WireGuardPeer

[各資源的設定](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

定義已宣告 WireGuardInterface 上的對等端。AllowedIPs 設定 WireGuard 的加密路由；此控制器不會自動建立一般 IP 路由。

## 指定條件與參照關係

- 對等端是介面完整 wg setconf 設定的一部分，並非獨立服務。同 metadata.name 的明確對等端會覆蓋 peersFrom 產生的對等端，即使 interface 不同。
- presharedKey 優先於 presharedKeyFile，兩者不互斥。只有未直接指定時才讀檔並移除首尾空白；缺少、無法讀取或空檔會使實際套用失敗。
- Connected 表示觀察到交握時間戳，經過時間另行回報；不保證交握是最近發生或目的地可達。有 endpoint 但無交握時為 Waiting/NoHandshakeYet。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 選填; string
    presharedKeyFile: "<string>" # 選填; string
    publicKey: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填，同一 Router 中既有 WireGuardInterface 的 metadata.name，僅填 OS ifname 不足以建立參照。 |
| `spec.publicKey` | 必填的對等端 WireGuard Base64 公開金鑰。驗證僅檢查非空，不解碼確認長度；套用時由 wg 檢查。 |
| `spec.allowedIPs` | 必填且不可為空的 IPv4/IPv6 CIDR 清單，控制送出封包的對等端選擇及接收封包允許的來源。不會補入前綴或建立一般路由。 |
| `spec.endpoint` | 選用的對等端 host:UDP-port，IPv6 使用 [IPv6]:port。空值省略 Endpoint，可從收到的封包學習端點。routerd 設定驗證不檢查語法或 DNS 可達性，套用時由 wg 處理。 |
| `spec.persistentKeepalive` | 持續保活封包間隔秒數，整數 0–65535；預設 0 表示停用。不是交握成功期限。 |
| `spec.presharedKey` | 選用的額外對稱金鑰，使用 WireGuard Base64 格式，預設無。與 presharedKeyFile 併用不報互斥錯誤，優先使用此值。 |
| `spec.presharedKeyFile` | 選用的路由器本機檔案，內容為共用金鑰本身，不是環境變數檔。缺少時不會產生金鑰，僅在 presharedKey 為空時讀取。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
