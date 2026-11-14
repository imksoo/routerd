# FirewallFlowPinhole

[各資源的設定](../resources.md#firewallflowpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

為防火牆區域配對學習允許的返回 IPv4 UDP 端點，不轉換位址。僅 Linux nftables，目前 PF 省略此資源。

## 指定條件與參照關係

- fromZone/toZone 必須為不同既有 FirewallZone 的單純名稱，不允許 self。outbound/inbound 與至少一個來源選擇器必填，所有 IPAddressSet 參照須存在。
- 送出符合時更新具期限集合後繼續後續規則，不自動允許。反向流量須符合交換後的來源/目的限制、允許遠端 UDP 來源連接埠及學習組合，之後在一般明確區域規則前允許。
- 預設 remoteAddress 的鍵為遠端 IPv4 + 本機 IPv4 + 本機 UDP 連接埠。localEndpoint 從鍵中省略遠端位址，但設定的遠端 CIDR/集合仍限制入站來源。遠端 UDP 目的連接埠不在學習鍵內。
- 學習項目是暫時核心集合，重建防火牆表或重啟會遺失。既有 conntrack 允許與控制例外可能先執行，不能取代完整防火牆策略。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 選填; object
      key: "" # 選填; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必填; string
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
    toZone: "<string>" # 必填; string
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
| `spec.fromZone` | 必填送出來源的既有 FirewallZone metadata.name，須與 toZone 不同，拒絕 self。 |
| `spec.toZone` | 必填送出目的的既有 FirewallZone metadata.name，返回檢查時成為輸入來源，不允許 self。 |
| `spec.correlation` | 選用組合鍵選擇，預設 remoteAddress，不是資源參照。 |
| `spec.correlation.key` | remoteAddress（預設）或 localEndpoint。前者包含遠端 IPv4，後者僅本機 IPv4 + UDP 連接埠。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`
