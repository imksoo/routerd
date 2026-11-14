# TailscaleNode

[各資源的設定](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

透過 routerd 產生的 Linux systemd oneshot 或 FreeBSD rc.d 指令稿管理 Tailscale 註冊選項，再讀取 tailscale status --json。Tailscale/Headscale 伺服器端核准仍需另行處理。

## 指定條件與參照關係

- authKey 與 authKeyEnv、authKeyFile 互斥。authKeyEnv 可與 authKeyFile 搭配，指定從檔案讀取的變數名稱；僅提供檔案時使用 TS_AUTHKEY。
- authKeyFile 是環境變數指派檔，不是原始金鑰，使用 TS_AUTHKEY=tskey-... 或 authKeyEnv 指定的變數。FreeBSD 每行使用一個不加引號的指派；選定變數缺少、重複或為空會失敗。Linux 以 systemd EnvironmentFile 載入。
- present 節點會保留 UDP 41641，拒絕與其他已設定監聽衝突。產生的命令為 tailscale up --timeout=30s。Applied/Rendered 表示服務套用，只有觀察到 BackendState=Running 才回報 Running，均不保證廣告子網可達。
- FreeBSD rc.d 拒絕變更缺少 routerd 管理標記的既有 tailscaled 程序。Linux 使用共用 tailscaled.service 與 routerd-tailscale-<name>.service 註冊單元。state: absent 移除註冊單元，不代表解除安裝套件或刪除 tailnet 節點。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 選填; boolean
    acceptRoutes: true # 選填; boolean
    advertiseExitNode: true # 選填; boolean
    advertiseRoutes: # 選填; array
      - "<string>" # string
    advertiseTags: # 選填; array
      - "<string>" # string
    authKey: "<string>" # 選填; string
    authKeyEnv: "<string>" # 選填; string
    authKeyFile: "<string>" # 選填; string
    hostname: "<string>" # 選填; string
    loginServer: "<string>" # 選填; string
    shieldsUp: true # 選填; boolean
    ssh: true # 選填; boolean
    state: "" # 選填; string; enum=["", "present", "absent"]
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
| `spec.state` | 預設 present；absent 移除管理的註冊服務，不移除套件或刪除控制伺服器上的節點。 |
| `spec.hostname` | 選用的 tailscale up --hostname 值，不是 Router.metadata.name，也不變更 OS 主機名稱。省略時不傳入參數。 |
| `spec.loginServer` | 選用的 --login-server 控制伺服器 URL，例如 Headscale。省略時依 Tailscale 用戶端設定或預設值；routerd 不提供 URL 預設值或資源查找。 |
| `spec.authKey` | 選用的直接註冊金鑰，與 authKeyEnv/authKeyFile 互斥。若未設定其他來源則不傳入金鑰參數，依既有登入狀態或用戶端註冊需求處理。 |
| `spec.authKeyEnv` | 選用的註冊金鑰環境變數名稱，不是金鑰本身。可指定 authKeyFile 中的變數。Linux 使用產生服務的環境，不會自動複製 routerd 程序環境。 |
| `spec.authKeyFile` | 選用的路由器本機環境檔絕對路徑，預設讀取 TS_AUTHKEY。跨 Linux/FreeBSD 請用不加引號的 NAME=value 格式。不是 Secret 資源參照。 |
| `spec.advertiseExitNode` | 預設 false；true 傳入 --advertise-exit-node。不會在控制伺服器核准出口節點，也不會讓本機改用其他出口節點。 |
| `spec.advertiseRoutes` | 選用 IPv4/IPv6 CIDR 清單，以逗號分隔傳入 --advertise-routes。空清單省略參數，而非明確傳入空值。控制伺服器所需核准另行處理。 |
| `spec.advertiseTags` | 選用標籤字串，以逗號分隔傳入 --advertise-tags。驗證拒絕空值及空白字元，但不強制 tag: 前綴或查驗伺服器 ACL 權限。空清單省略參數。 |
| `spec.acceptRoutes` | 選用的 --accept-routes 布林值。省略時不傳入參數，由 Tailscale 用戶端決定；明確 false 仍會傳入，與省略不同。 |
| `spec.acceptDNS` | 選用的 --accept-dns 布林值，省略時不強制 true 或 false，僅傳入明確值。不會設定 routerd DNSResolver/DNSForwarder 資源。 |
| `spec.ssh` | 預設 false 省略參數；true 傳入 --ssh 啟用 Tailscale SSH 要求。與 OS sshd 及 routerd 管理存取不同。 |
| `spec.shieldsUp` | 選用的 --shields-up 布林值，省略不傳入參數，明確 true/false 才傳入。 |
| `spec.when` | 選用的共用啟用條件，省略表示無條件。state/all/any 及欄位參照規則請見共用欄位。控制產生服務，不會刪除 tailnet 節點。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/tailscale_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/tailscale_systemd.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/tailscale/tailscale.go](https://github.com/imksoo/routerd/blob/main/pkg/tailscale/tailscale.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
