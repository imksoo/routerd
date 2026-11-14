# WireGuardPeer

[各資源的設定](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

定義已宣告 WireGuardInterface 上的對等端。AllowedIPs 設定 WireGuard 的加密路由；此控制器不會自動建立一般 IP 路由。

## 指定條件與參照關係

- 對等端是介面完整 wg setconf 設定的一部分，並非獨立服務。同 metadata.name 的明確對等端會覆蓋 peersFrom 產生的對等端，即使 interface 不同。
- presharedKey 優先於 presharedKeyFile，兩者不互斥。只有未直接指定時才讀檔並移除首尾空白；缺少、無法讀取或空檔會使實際套用失敗。
- Connected 表示觀察到交握時間戳，經過時間另行回報；不保證交握是最近發生或目的地可達。有 endpoint 但無交握時為 Waiting/NoHandshakeYet。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs:
      - <string>
    [ endpoint: <string> ]
    interface: <string>
    [ persistentKeepalive: <integer> ]
    [ presharedKey: <string> ]
    [ presharedKeyFile: <string> ]
    publicKey: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.interface` | **必填** | string | 必填，同一 Router 中既有 WireGuardInterface 的 metadata.name，僅填 OS ifname 不足以建立參照。 |
| `spec.publicKey` | **必填** | string | 必填的對等端 WireGuard Base64 公開金鑰。驗證僅檢查非空，不解碼確認長度；套用時由 wg 檢查。 |
| `spec.allowedIPs` | **必填** | array | 必填且不可為空的 IPv4/IPv6 CIDR 清單，控制送出封包的對等端選擇及接收封包允許的來源。不會補入前綴或建立一般路由。 |
| `spec.endpoint` | *選填* | string | 選用的對等端 host:UDP-port，IPv6 使用 [IPv6]:port。空值省略 Endpoint，可從收到的封包學習端點。routerd 設定驗證不檢查語法或 DNS 可達性，套用時由 wg 處理。 |
| `spec.persistentKeepalive` | *選填* | integer; minimum=0; maximum=65535 | 持續保活封包間隔秒數，整數 0–65535；預設 0 表示停用。不是交握成功期限。 |
| `spec.presharedKey` | *選填* | string | 選用的額外對稱金鑰，使用 WireGuard Base64 格式，預設無。與 presharedKeyFile 併用不報互斥錯誤，優先使用此值。 |
| `spec.presharedKeyFile` | *選填* | string | 選用的路由器本機檔案，內容為共用金鑰本身，不是環境變數檔。缺少時不會產生金鑰，僅在 presharedKey 為空時讀取。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `latestEndpoint`, `latestHandshake`, `handshakeAgeSeconds`, `transferRxBytes`, `transferTxBytes`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
