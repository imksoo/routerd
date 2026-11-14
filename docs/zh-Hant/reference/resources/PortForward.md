# PortForward

[各資源的設定](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

將 TCP/UDP 連接埠轉送至單一 IPv4 端點，可選內部迴送 NAT。Linux 產生 DNAT 與轉送允許，FreeBSD 使用 rdr pass。

## 指定條件與參照關係

- listen/target 必填。target 需要且只能提供 address/addressFrom 其中之一與 port；固定目標必須是純 IPv4，不接受主機名或 CIDR。listen 兩者皆省略時比對介面所有目的。
- 參照須存在並提供對應欄位，但 NAT 產生器只讀宣告的 IPv4StaticAddress.address/VirtualAddress.address。optional=true 可在支援來源未解析時省略規則，不會啟用任意動態來源。PortForward 沒有後端健康檢查。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 選填; object
      enabled: true # 選填; boolean
      interfaces: # 選填; array
        - "<string>" # string
      mode: "" # 選填; string; enum=["", "auto", "manual", "off"]
    listen: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    target: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
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
| `spec.listen` | 必填 IPv4 入口端點，建立 NAT/防火牆規則，不建立應用程式監聽或指派位址。 |
| `spec.listen.interface` | 必填單純已宣告邏輯介面名稱，不是 Kind/name。須存在設定索引並可解析 OS ifname。 |
| `spec.listen.address` | 選用純 IPv4 目的位址，與 addressFrom 互斥。省略時比對監聽介面上任意目的位址。 |
| `spec.listen.addressFrom` | 選用共用 resource/field/optional 參照，與 address 互斥。NAT/靜態產生支援宣告的 IPv4StaticAddress.address 或 VirtualAddress.address，不讀任意執行期狀態。一般參照驗證通過不保證產生器支援。 |
| `spec.listen.protocol` | 必填 tcp/udp，無預設值。選擇轉送流量，不決定健康檢查協定。 |
| `spec.listen.port` | 必填目的連接埠 1–65535，無範圍、清單或預設值。 |
| `spec.hairpin` | 選用經公開位址返回內部的 NAT 設定，啟用時需可解析 listen 位址。PortForward 預設 off，IngressService 預設 auto。 |
| `spec.hairpin.enabled` | 預設 false。mode 省略時 true 讓 PortForward 使用 manual，IngressService 仍預設 auto。明確 off 即使 enabled=true 也停用。 |
| `spec.hairpin.mode` | auto/manual/off。auto 在 listen 與後端同屬宣告 IPv4 子網或相同私有 /24 時選 listen 介面，不探索任意 LAN 路由。manual 需 enabled=true 與 interfaces 才產生規則。 |
| `spec.hairpin.interfaces` | 選用單純宣告邏輯介面名，不可重複，啟用 manual 時需非空。auto 可另加入 listen 介面。IPv4 DNAT 加 masquerade/PF SNAT 讓回程經路由器。 |
| `spec.when` | 選用共用啟用條件，預設無條件。不成立時從有效產生排除；條件與結構描述限制請見共用欄位。 |
| `spec.target` | 必填單一內部端點，不指派 IP 或啟動服務。 |
| `spec.target.address` | 純 IPv4 目標，與 target.addressFrom 必須擇一，無預設。 |
| `spec.target.addressFrom` | 共用 resource/field/optional 參照，與 listen.addressFrom 相同宣告欄位限制，須解析為 IPv4。 |
| `spec.target.port` | 必填內部 TCP/UDP 連接埠 1–65535，獨立於 listen.port。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `listenAddress`, `target`, `dryRun`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
