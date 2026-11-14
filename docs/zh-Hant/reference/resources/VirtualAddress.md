# VirtualAddress

[各資源的設定](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

維護靜態或選舉後的浮動位址。Linux 靜態使用 iproute2、VRRP 使用 keepalived；FreeBSD 使用 ifconfig/CARP。

## 指定條件與參照關係

- family/interface 與 address/addressFrom 擇一必填。固定 IPv4 須 /32，IPv6 VRRP/CARP 須 /128；靜態 IPv6 可保留其他前綴長度。addressFrom 的純位址提升為 /32 或 /128。
- interface 須存在設定介面索引。同介面上不可同時由 IPv4StaticAddress 與 VirtualAddress 管理相同索引位址；addressFrom 指向同介面的 IPv4StaticAddress 也會拒絕。VRRP ID 須依 family/interface 唯一。
- addressFrom 經一般來源/欄位驗證，但位址產生器僅讀宣告的 IPv4StaticAddress.address/VirtualAddress.address，不讀任意執行期狀態。optional 不允許最終 VIP 為空，可能出現 Error/Pending。
- mode=vrrp 需要 virtualRouterID，Linux 另需非空單播 peers。FreeBSD 使用原生 CARP，不將 peers 作單播目標。省略 preempt 停用搶占；CARP 搶占為主機全域，任一實例 true 即啟用 sysctl。
- 容錯 VMAC 依同一 VIP 的權威選舉，不另建 VRRP 選舉。Linux 專用輔助程式執行時再驗證名稱/MAC/鏈路本地值；additionalFailoverVMACs 的 YAML 驗證較主項目弱。產生/套用時需要有效父介面參照與已安裝輔助程式。
- gracefulActivation 僅 Linux IPv4 VRRP。選舉/VMAC 可先於 VIP 公布，準備條件允許 VIP 加入。逾時只報失敗並持續保留與重試，絕不強制公布。VIP 成功公布後，只要仍為 master，後續準備條件退化不撤回 VIP。
- VRRP 公告時間由實作決定（通常 1 秒），不透過 advertInterval/preemptDelay 設定；這些舊成員排除於 YAML/JSON 結構描述。協定優先權/驗證仍依 keepalived/CARP。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 選填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 選填; boolean
    hostname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    mode: "" # 選填; string; enum=["", "static", "vrrp"]
    track: # 選填; array
      - # object
        confirmConsecutiveHealthy: 1 # 選填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 選填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 選填; integer; minimum=0; maximum=254
    vrrp: # 選填; object
      additionalFailoverVMACs: # 選填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 選填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 選填; boolean
      authentication: "<string>" # 選填; string
      authenticationFrom: # 選填; object
        base64: true # 選填; boolean
        env: "<string>" # 選填; string
        file: "<string>" # 選填; string
      failoverVMAC: # 選填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 選填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 選填; boolean
      gracefulActivation: # 選填; object
        readyWhen: # 必填; object
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
        timeout: "<string>" # 選填; string
      peers: # 選填; array
        - "<string>" # string
      preempt: true # 選填; boolean
      priority: 1 # 選填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
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
| `spec.family` | 必填 ipv4/ipv6，無預設，須與位址族一致。 |
| `spec.interface` | 必填單純宣告邏輯介面名稱，解析為 OS 裝置，不是 Kind/name。 |
| `spec.address` | 與 addressFrom 互斥的固定位址前綴。IPv4 /32；IPv6 除靜態模式外 /128。語意驗證允許 addressFrom 替代，但 JSON Schema 將 address 標為必填。 |
| `spec.addressFrom` | 與 address 互斥的共用來源，產生器支援宣告 IPv4StaticAddress.address/VirtualAddress.address，須明確 field。不遞迴解析巢狀 VirtualAddress 來源。 |
| `spec.hostname` | 選用 FQDN，用於已服務 DNSZone 的自動本機 DNS 紀錄與狀態。不改 OS 主機名或建立外部 DNS 紀錄。 |
| `spec.externalDNS` | 預設 false，true 略過自動本機名稱紀錄與 DNS 範圍警告，不連絡外部 DNS 供應商。 |
| `spec.mode` | static（預設）或 vrrp。static 不選舉直接套用本機；vrrp 使用 Linux keepalived/FreeBSD CARP。VRRP/track 物件不限制靜態持有。 |
| `spec.gratuitousARP` | 預設 false，僅 Linux 靜態 IPv4 可 true。新增/恢復遺失 VIP 後執行 arping -U -c 3，一般協調不反覆公告。失敗會報告並重試。 |
| `spec.when` | 選用共用資源啟用條件，預設無條件。篩選整個資源，與 gracefulActivation.readyWhen 的加入閘門不同。 |
| `spec.vrrp` | 僅 mode=vrrp 使用的選舉設定。靜態模式忽略，且不驗證所有內層值。 |
| `spec.vrrp.virtualRouterID` | vrrp 必填 1–255，映射 CARP vhid。同 Router 的 family/interface 內唯一，同次選舉的節點須一致。 |
| `spec.vrrp.priority` | 1–254，省略/0 為 100。追蹤扣分後至少 1；CARP 轉換為 advskew=max(1,254-priority)。 |
| `spec.vrrp.preempt` | 省略/false 不僅因較高優先權奪回持有；true 啟用搶占。CARP 使用主機全域 net.inet.carp.preempt，任一設定實例 true 即啟用。 |
| `spec.vrrp.peers` | Linux vrrp 必填非空對等位址/主機名清單。語法驗證，產生時去重排序。FreeBSD CARP 不將此清單作對等設定。 |
| `spec.vrrp.authentication` | 選用明文驗證值，預設無，與 authenticationFrom 互斥。Linux 產生 keepalived auth_type PASS，CARP 使用 pass。是協定驗證，不是加密 VRRP 流量。 |
| `spec.vrrp.authenticationFrom` | 選用密鑰來源，與明文 authentication 互斥。設定時 file/env 必須擇一。產生時讀取，遺失檔案/環境或無效 Base64 會失敗；解析空值會省略驗證。 |
| `spec.vrrp.authenticationFrom.file` | 路由器上的密鑰檔案路徑，與 env 互斥。產生器移除尾端 CR/LF，不移除全部前後空格。 |
| `spec.vrrp.authenticationFrom.env` | routerd 程序的環境變數名稱，與 file 互斥。不是 shell 展開或遠端環境變數。 |
| `spec.vrrp.authenticationFrom.base64` | 預設 false。true 在移除編碼值前後空白後解碼標準 Base64，再移除解碼結果尾端 CR/LF。 |
| `spec.vrrp.failoverVMAC` | 選用由同一 VRRP 選舉控制的 Linux macvlan L2 身分，不另選舉。主項目在 Linux VRRP 模式驗證，FreeBSD 不支援。 |
| `spec.vrrp.additionalFailoverVMACs` | 選用依相同角色轉換的額外 VMAC，預設空，欄位同 failoverVMAC。目前 YAML 驗證不完整，Linux 輔助程式/產生仍需有效父介面、名稱與 Ethernet MAC。 |
| `spec.vrrp.gracefulActivation` | 選用 Linux IPv4 VRRP 加入閘門，預設無；其他位址族/FreeBSD 拒絕。選舉後準備相依狀態並等待公布 VIP。 |
| `spec.vrrp.gracefulActivation.readyWhen` | 提供閘門時必填非空共用 state/all/any 條件。驗證參照與條件，只控制初次公布，不控制成功公布後的持續撤回。 |
| `spec.vrrp.gracefulActivation.timeout` | 嚴格正 Go 時間長度，預設 45s。到期只將啟用狀態改為 Failed/ReadinessTimeout，不強制公布、不停止重試或重啟路由器。 |
| `spec.track` | 選用資源健康追蹤清單，預設空，僅 vrrp。異常扣分累加影響選舉優先權，不直接移除靜態 VIP。 |
| `spec.track[].resource` | 必填 Kind/name 格式，此處驗證格式不驗存在。讀取 phase，一般就緒 phase 為健康，BGPRouter/BGPPeer 需 Established。Disabled/Standby/NotApplicable 與 when=false 為中立，保留目前扣分決定。 |
| `spec.track[].unhealthyPenalty` | 0–254，省略/0 為 50，零無法停用扣分。確認異常後套用，確認健康後移除。 |
| `spec.track[].confirmConsecutiveUnhealthy` | 扣分前連續異常評估次數，省略/0 為 3，明確 1–255。是協調觀測次數，不是秒數。 |
| `spec.track[].confirmConsecutiveHealthy` | 解除扣分前連續健康次數，省略/0 為 2，明確 1–255。追蹤決定/計數可在重啟後由保存狀態還原。 |
| `spec.vrrp.failoverVMAC.parentInterface` | 必填設定的邏輯父介面，產生時解析為 OS 名稱，不是狀態來源。 |
| `spec.vrrp.additionalFailoverVMACs[].parentInterface` | 必填設定的邏輯父介面，產生時解析為 OS 名稱，不是狀態來源。 |
| `spec.vrrp.failoverVMAC.interface` | 必填 Linux VMAC 裝置名稱，最多 15 字元且須有效介面語法，由角色輔助程式建立/啟用/停用。 |
| `spec.vrrp.additionalFailoverVMACs[].interface` | 必填 Linux VMAC 裝置名稱，最多 15 字元且須有效介面語法，由角色輔助程式建立/啟用/停用。 |
| `spec.vrrp.failoverVMAC.macAddress` | 必填六位元組 Ethernet MAC，共用切換身分的節點須一致，不從 VRID 自動產生。 |
| `spec.vrrp.additionalFailoverVMACs[].macAddress` | 必填六位元組 Ethernet MAC，共用切換身分的節點須一致，不從 VRID 自動產生。 |
| `spec.vrrp.failoverVMAC.linkLocalAddress` | 選用純 IPv6 鏈路本地身分（fe80::/10），不含前綴/區域後綴。Linux 輔助程式呼叫時驗證，省略採一般鏈路本地處理。 |
| `spec.vrrp.additionalFailoverVMACs[].linkLocalAddress` | 選用純 IPv6 鏈路本地身分（fe80::/10），不含前綴/區域後綴。Linux 輔助程式呼叫時驗證，省略採一般鏈路本地處理。 |
| `spec.vrrp.failoverVMAC.withdrawRouterAdvertisement` | 預設 false。多 VMAC 引數形式中 true 會在停用前以鏈路本地來源送出 router lifetime=0 RA。未明確 linkLocalAddress 的單 VMAC 舊引數形式不傳遞此旗標。 |
| `spec.vrrp.additionalFailoverVMACs[].withdrawRouterAdvertisement` | 預設 false。多 VMAC 引數形式中 true 會在停用前以鏈路本地來源送出 router lifetime=0 RA。未明確 linkLocalAddress 的單 VMAC 舊引數形式不傳遞此旗標。 |
| `spec.vrrp.gracefulActivation.readyWhen.state` | readyWhen 內的選用狀態鍵條件對應表，所有項目皆須符合。state/all/any 須擇一且非空。equals/in/contains/exists/status/for/maxAge 遵循共用欄位；資源形式的鍵須參照已宣告資源及提供欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/vrrp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/controller.go) · [pkg/controller/vrrp/backend.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/backend.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/vrrp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/vrrp.go) · [pkg/render/carp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/carp.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/secrets.go](https://github.com/imksoo/routerd/blob/main/pkg/render/secrets.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
