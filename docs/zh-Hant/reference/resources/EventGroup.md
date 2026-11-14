# EventGroup

[各資源的設定](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

宣告共用的聯邦事件群組，以及此路由器的識別、對等節點來源、接收與本機保留設定。

## 指定條件與參照關係

- nodeName 必填，metadata.name 為群組識別；參與節點使用相同群組名與共用密鑰。僅識別用途可省略 secretFile，但傳輸輔助程式僅在提供 secretFile 時啟動。Applied 表示執行設定已寫入，不保證事件已送達。
- 設定驗證在 listen.address、peersFrom 或參照此群組的 EventPeer 存在時要求 secretFile，auth.secretRef 不能替代。eventd 啟動時檔案須存在，移除前後空白後的密鑰不可為空。
- 接收器是否啟動由 listen.port > 0 決定，並非只看 address。使用正連接埠時請明確指定地址；地址空白而連接埠為正可能綁定所有地址。連接埠 0 停用 TCP 接收，但可繼續推送與清理。
- peersFrom 匯入 SAMNodeSet 中有 eventEndpoint 的節點，排除 nodeRef 等於 nodeName 的自身節點。依遠端 nodeName 去重，後來源覆寫前來源，最後由靜態 EventPeer 覆寫匯入節點。必要來源遺失時維持 Pending 並保留舊設定；選用來源遺失則略過。
- eventd 在啟動及定期處理（預設 1 分鐘）執行本機清理。時間與數量限制同時適用；時間依 observedAt，數量保留最新觀測事件。這與每個事件的 expiresAt 及本機 EventRule 紀錄保留不同。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 選填; object
      mode: "" # 選填; string; enum=["", "hmac"]
      secretFile: "<string>" # 選填; string
      secretRef: "<string>" # 選填; string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 0 # 選填; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必填; string
    peersFrom: # 選填; array
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    replayWindow: "<string>" # 選填; string
    retention: # 選填; object
      maxAge: "<string>" # 選填; string
      maxEvents: 0 # 選填; integer; minimum=0
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
| `spec.nodeName` | 必填且非空的本機節點識別，標記於本機產生的聯邦事件。無主機名稱預設；搭配 peersFrom 時須符合 SAMNodeSet.nodeRef。 |
| `spec.peersFrom` | 選用的有序來源清單，預設空。即使所有來源為選用或沒有節點，提供此欄位仍需要 auth.secretFile。 |
| `spec.peersFrom[].resource` | 必填 SAMNodeSet/<name> 參照。驗證格式；來源未宣告時依執行期 optional/Pending 處理，不當作一般狀態值參照拒絕。 |
| `spec.peersFrom[].optional` | 預設 false：遺失來源會阻止新設定。true 略過遺失來源，但格式錯誤或無法解碼仍失敗。 |
| `spec.retention` | 選用的本機聯邦事件保留設定，預設無時間或數量限制。僅在 eventd 清理執行時生效。 |
| `spec.retention.maxEvents` | 每群組的非負事件數上限，預設 0 為無限。正值在時間清理後保留 observedAt/id 最新的紀錄。 |
| `spec.retention.maxAge` | 選用 Go 時間長度，預設空為無限。零或負值可通過驗證，執行設定會省略，亦表示無時間限制。 |
| `spec.auth` | 選用的傳輸完整性設定。目前傳輸一律使用 HMAC，沒有未驗證模式。 |
| `spec.auth.mode` | 接受空或 hmac，預設空。傳輸執行時兩者皆使用 HMAC，控制器不以此選擇其他方案。 |
| `spec.auth.secretRef` | 預設空。接受但目前控制器與輔助程式不解析；不能替代 secretFile，也不作資源參照驗證。 |
| `spec.auth.secretFile` | 共用 HMAC 密鑰檔案路徑。空值停用傳輸輔助程式；指定接收地址或對等節點時依上述規則必填。不會自動分發密鑰。 |
| `spec.listen` | 選用 HTTP 接收綁定，預設地址空、連接埠 0。接收時請明確設定兩者。 |
| `spec.listen.address` | 接收綁定地址，預設空。非空時需要 1–65535 連接埠與 secretFile。輔助程式不會僅因地址空而停用接收，請見連接埠規則。 |
| `spec.listen.port` | 整數 0–65535，預設 0 停用接收。正值在 address 啟用純 HTTP；本資源沒有 TLS 憑證欄位。 |
| `spec.replayWindow` | 簽署請求時間戳偏差容許範圍，使用 Go 時間長度。省略、零或負值為 5m。不是事件保留時間或訊息去重 TTL。 |
| `spec.when` | 選用的共用啟用條件，預設無條件。有效設定篩選控制傳輸設定與服務生命週期；條件與結構描述限制請見共用欄位。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
