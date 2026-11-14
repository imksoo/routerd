# NTPClient

[各資源的設定](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

管理或觀察向上游同步時間的用戶端。

## 指定條件與參照關係

- managed=true須宣告servers/serverFrom/fallbackServers至少一項，static還要求servers本身。無法解析上游時用戶端為Pending/NoServers；管理chrony時停用系統systemd-timesyncd。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # 選填; array
      - "<string>" # string
    interface: "<string>" # 選填; string
    managed: true # 選填; boolean
    provider: "systemd-timesyncd" # 選填; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    servers: # 選填; array
      - "<string>" # string
    source: "static" # 選填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.provider` | 用戶端實作，通常預設systemd-timesyncd，FreeBSD預設ntpd，也接受chrony。ntpd需要FreeBSD rc.d，systemd-timesyncd需要平台支援。 |
| `spec.managed` | 預設false，只記錄Observed，不設定或啟動時間服務；管理檔案與服務時設true。 |
| `spec.source` | 預設static，使用servers。auto/dhcp/dhcpv6皆依serverFrom解析值、fallbackServers、servers的順序選取，不會自動探索DHCP來源。 |
| `spec.interface` | 選填邏輯介面名稱，預設空；指定時Router整體驗證要求存在。NTP controller不用它選來源或綁定服務。 |
| `spec.servers` | 上游主機名稱或IP，預設空；managed=true且source省略或static時必填。各項須為非空白單一值，不是資源參照。 |
| `spec.serverFrom` | 明確值來源清單，預設空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。只用於auto/dhcp/dhcpv6，每個resource與field須存在。 此解析器不使用optional旗標，缺值即選下一種替代來源。 |
| `spec.fallbackServers` | 備用上游，預設空；動態來源的serverFrom無值時採用。systemd-timesyncd還會產生FallbackNTP，chrony與ntpd不產生獨立備用指令。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
