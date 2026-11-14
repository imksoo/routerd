# PPPoESession

[各資源的設定](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

監督執行 routerd-pppoe-client，在 Linux 使用 pppd、FreeBSD 使用 mpd5，並公開協商的 IPv4 工作階段狀態。部分舊欄位僅影響手動 FreeBSD render 輸出，不影響此精靈路徑。

## 指定條件與參照關係

- interface 必須參照同一 Router 的基本 Interface，不可使用 PPPoESession、隧道等作為底層介面。即使 enabled:false，仍需 username 及 password/passwordFile 其中一項。
- 實際監督流程傳入 authMethod、mtu/mru、serviceName/acName、lcpEchoInterval/lcpEchoFailure，不傳入 ifname、managed、persist、ipv6、usePeerDNS、lcpInterval/lcpFailure。請勿由另一條手動 FreeBSD render 路徑推定實際行為。
- 主精靈在兩個平台預設 MTU/MRU 1454、LCP echo 30 秒/4 次。獨立的手動 FreeBSD mpd5 產生路徑預設 1492、舊 LCP 20 秒/3 次。設定計畫或 Connected 不代表上游實際可達。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 選填; string
    authMethod: "chap" # 選填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 選填; boolean
    enabled: true # 選填; boolean
    ifname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    ipv6: true # 選填; boolean
    lcpEchoFailure: 0 # 選填; integer; minimum=0
    lcpEchoInterval: 0 # 選填; integer; minimum=0
    lcpFailure: 0 # 選填; integer; minimum=0
    lcpInterval: 0 # 選填; integer; minimum=0
    managed: true # 選填; boolean
    mru: 576 # 選填; integer; minimum=576; maximum=1500
    mtu: 576 # 選填; integer; minimum=576; maximum=1500
    password: "<string>" # 選填; string
    passwordFile: "<string>" # 選填; string
    persist: true # 選填; boolean
    secretEncoding: "plain" # 選填; string; enum=["plain"]
    serviceName: "<string>" # 選填; string
    usePeerDNS: true # 選填; boolean
    username: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填的底層基本 Interface metadata.name，解析為實體 OS 名稱，不是狀態值來源。 |
| `spec.ifname` | 選用的 PPP 名稱，最多 15 字元，不含空白或斜線；省略時 ppp-<name> 須在 15 字元內。目前主監督流程忽略此欄位，由精靈使用 DefaultIfName；手動 FreeBSD render 才使用此值。實際運作請勿依賴自訂名稱。 |
| `spec.enabled` | 預設 true；false 不啟動並停止受監督的工作階段，回報 Disabled，但不略過設定驗證。 |
| `spec.authMethod` | 預設 chap，也接受 pap/both，控制實際 pppd/mpd5 設定中允許的 PAP/CHAP 認證。 |
| `spec.username` | 必填的 PPP 認證使用者名稱，無衍生預設值。 |
| `spec.password` | 直接指定明文 PPP 密碼，未提供 passwordFile 時必填，與 passwordFile 互斥。 |
| `spec.passwordFile` | 路由器本機原始密碼檔，未提供 password 時必填。精靈啟動時讀取並移除首尾空白；缺少、無法讀取或為空會失敗，不是環境變數指派檔。 |
| `spec.mtu` | 選用 576–1500。省略或 0 時主精靈為 1454，手動 FreeBSD render 為 1492。PPP MTU 1500 需底層支援，驗證不保證可用。 |
| `spec.mru` | 選用接收上限 576–1500；省略或 0 時主精靈為 1454，手動 FreeBSD render 為 1492。 |
| `spec.serviceName` | 選用 PPPoE 服務選擇名稱，空值不明確指定，不接受換行。 |
| `spec.acName` | 選用的存取集中器選擇名稱，空值不明確指定，不接受換行。 |
| `spec.defaultRoute` | 預設 false。true 時主路由清理保留觀察到的 PPP 裝置既有預設路由，但精靈設定不建立該路由。手動 FreeBSD render 會輸出預設路由指令。實際路徑若需自動安裝，請明確設定 IPv4Route。 |
| `spec.usePeerDNS` | 預設 false，但主精靈路徑不讀取。LinuxPeer 一律輸出 usepeerdns，協商 DNS 會公開於 PPP 狀態；此欄位不控制 routerd DNS 轉送或 resolv.conf 套用。 |
| `spec.persist` | 選用布林值，目前受監督精靈設定不使用；省略不會建立此資源的重試原則，監督生命週期另行處理。 |
| `spec.lcpInterval` | 僅手動 FreeBSD render 使用的舊式非負間隔，省略或 0 為 20 秒。主精靈忽略，請用 lcpEchoInterval。 |
| `spec.lcpFailure` | 僅手動 FreeBSD render 使用的舊式非負失敗倍數，省略或 0 為 3。主精靈忽略，請用 lcpEchoFailure。 |
| `spec.lcpEchoInterval` | 非負的 LCP echo 間隔秒數，省略或 0 使用主精靈預設 30 秒，不表示停用 echo。手動 FreeBSD render 忽略。 |
| `spec.lcpEchoFailure` | 非負的 LCP echo 失敗次數，省略或 0 使用主預設 4。Linux 傳入 lcp-echo-failure，FreeBSD 精靈產生 keep-alive 間隔及間隔×次數。 |
| `spec.ipv6` | 預設 false，目前不傳入受監督 PPP 精靈，其產生設定不輸出 IPv6 協商選項，請勿依賴此旗標啟用 PPP IPv6。 |
| `spec.managed` | 預設 false，但主監督流程不論此值都啟動 enabled 工作階段。僅手動 FreeBSD render 中，true 才將啟用的工作階段加入 mpd5 預設 load 清單。不是實際路徑的唯讀保護。 |
| `spec.secretEncoding` | 僅接受省略或 plain，不會對 password/passwordFile 執行 Base64 解碼或加密。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go) · [pkg/controller/pppoesession/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/pppoesession/controller.go) · [cmd/routerd-pppoe-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-pppoe-client/main.go) · [pkg/pppoeclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pppoeclient/client.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [cmd/routerd/cmd_render.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_render.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
