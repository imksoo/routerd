# NAT44FlowDNATPinhole

[各資源的設定](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

從送出 IPv4 UDP 學習內部端點，依收到 UDP 的目的連接埠動態 DNAT。僅 Linux nftables，目前 PF 未實作此資源。

## 指定條件與參照關係

- fromInterfaceRefs、outbound、inbound 與來源選擇器必填。介面與 IPAddressSet 參照須存在於同一 Router。僅 IPv4/UDP，屬於連接埠觸發 DNAT，不是一般 conntrack 回覆處理。
- 映射鍵為送出本機 UDP 來源連接埠，值為 SNAT 前內部 IPv4 與相同連接埠。所有選定介面共用每資源一個映射。不同用戶端使用相同來源連接埠會覆寫，遠端位址不在鍵內。
- 入站 DNAT 檢查選定輸入介面、允許的遠端來源連接埠與已學習目的連接埠。不會將送出目的 CIDR/集合重新套用到入站遠端位址，也不建立防火牆轉送允許；轉換流量仍需防火牆允許。
- 學習映射是核心執行狀態，不是設定或持久化資料庫。重建 NAT 表或重啟會遺失，需送出流量重新學習。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 選填; object
      key: "" # 選填; string; enum=["", "localPort"]
    fromInterfaceRefs: # 選填; array
      - "<string>" # string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 選填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 選填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 選填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 選填; string
    timeout: "<string>" # 選填; string
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
| `spec.outbound` | 必填的送出學習比對。記錄符合的 UDP 流量身分，但不會自行允許流量通過防火牆。 |
| `spec.outbound.sourceSetRef` | 選用既有 IPAddressSet/<name> 來源選擇器。此欄或 sourceCIDRs 至少一者必填；兩者可作 OR。預設空。 |
| `spec.outbound.sourceCIDRs` | 選用 IPv4 來源前綴 OR 清單，預設空，至少需要一個來源選擇器。拒絕 IPv6，產生時遮罩主機位元。 |
| `spec.outbound.destinationCIDRs` | 選用學習用 IPv4 遠端目的，預設空。與 destinationSetRefs 作 OR；兩者皆無時任意遠端位址。 |
| `spec.outbound.destinationSetRefs` | 選用既有 IPAddressSet/<name> 遠端目的參照，不可重複，預設空。來源與目的兩類以 AND 組合。 |
| `spec.outbound.protocol` | 必填 udp，無預設值。此學習機制不支援 TCP/IPv6。 |
| `spec.outbound.destinationPorts` | 必填非空送出 UDP 目的連接埠：數字 1–65535 或唯一 start-end 範圍。範圍不可與其他項目混用。 |
| `spec.inbound` | 必填返回流量比對。sourcePorts 選擇遠端 UDP 傳送者連接埠，不是本機目的連接埠。 |
| `spec.inbound.sourcePorts` | 必填非空遠端 UDP 來源連接埠清單，與 outbound.destinationPorts 相同的 1–65535/單獨範圍限制。 |
| `spec.timeout` | 選用嚴格為正的 Go 時間長度，預設 3m。每個符合送出封包更新學習項目。產生時四捨五入為秒且至少 1s；這是學習狀態到期，不是 UDP 協定逾時。 |
| `spec.when` | 選用共用啟用條件，預設無條件。篩除資源不產生學習映射/集合規則。巢狀條件與結構描述限制請見共用欄位。 |
| `spec.fromInterfaceRefs` | 必填非空輸出/輸入介面清單，接受單純名稱或 Interface/name、PPPoESession/name、DSLiteTunnel/name。送出比對 oifname，入站比對同清單的 iifname。 |
| `spec.correlation` | 選用關聯物件，僅支援 localPort，省略亦使用相同鍵。 |
| `spec.correlation.key` | 僅空或 localPort。產生器無其他分支，一律以 UDP 來源連接埠索引學習映射。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`
