---
title: 以 MAC 位址隔離訪客裝置
---

# 以 MAC 位址隔離訪客裝置

`ClientPolicy` 是 routerd 的訪客模式。
它依 MAC 位址對同一 LAN 上的裝置進行分類，並在一般區域間防火牆矩陣之前，優先套用更嚴格的轉送策略。

即使尚未劃分 VLAN，也能在受信任的裝置與僅需使用網際網路的裝置之間，建立明確的界線。

## 使用案例

常見用途如下：

- 在家庭環境中，不希望來訪者的智慧型手機、遊戲機、家電或外帶筆電能存取管理網路或家用伺服器。
- 在集合住宅或共用住宅中，預設將所有裝置視為訪客，只有明確指定的裝置才視為受信任。
- 隔離攝影機、HEMS、電視、喇叭等 IoT 裝置。允許其使用 DNS、DHCP、NTP 及網際網路，但阻止橫向通訊。
- 在小型辦公室的訪客網路中，共用物理 LAN，但封鎖 RFC 1918 及 ULA 目的地的流量。

完整範例請參閱 [examples/guest-mode.yaml](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)。

## 運作原理

在 Linux 上，routerd 將 `ClientPolicy` 產生至 nftables 的 `inet routerd_filter` 資料表。

每條策略會產生以下規則：

- 類似 `client_policy_guest_devices` 的 nftables `ether_addr` set。
- `mode: include` 使用 `ether saddr @set` 比對。
- `mode: exclude` 使用 `ether saddr != @set` 比對。
- 允許訪客裝置存取路由器上特定服務的 self 向許可規則。
- 封鎖私有 IPv4 及 ULA IPv6 目的地的轉送拒絕規則。
- 優先於拒絕規則的可選許可規則。

產生的規則會插入 `input` 鏈與 `forward` 鏈的較前位置。
因此，即使在角色矩陣下本可被 `trust -> self` 或 `trust -> trust` 允許的通訊，訪客裝置的流量也會更早被篩選。

`ClientPolicy` 並非 `FirewallZone` 的替代方案，一般模型照常使用：

- `FirewallZone` 決定介面的一般角色。
- `FirewallPolicy` 決定拒絕記錄等共通行為。
- `FirewallRule` 新增明確的例外。
- `ClientPolicy` 在 LAN 內疊加以裝置為單位的限制。

MAC 比對直接查看 Ethernet 來源位址，因此不需要 DHCP 租約。
不過，搭配 `DHCPv4Reservation` 使用，可讓裝置的 IPv4 位址、名稱及 Web 管理介面中的顯示更加穩定。

## 規格說明

`ClientPolicy` 屬於 `firewall.routerd.net/v1alpha1`。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - "18:ec:e7:33:12:6c"
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

| 欄位 | 必填 | 說明 |
| --- | --- | --- |
| `mode` | 是 | `include` 或 `exclude`。 |
| `interfaces` | 否 | 套用策略的 LAN 側 `Interface` 參照。`Interface/lan` 與 `lan` 指向相同介面。省略時套用至所有屬於 `trust` `FirewallZone` 的介面。 |
| `macs` | 否 | 縮短格式的 MAC 清單。include 模式視為訪客，exclude 模式視為受信任。 |
| `isolation` | 否 | 表達訪客的意圖。可在 `lanInternet`、`lanLAN`、`lanMgmt`、`mDNSBroadcast` 指定 `allow` 或 `deny`。 |
| `classification` | 否 | 結構化的用戶端分類條目。 |
| `classification[].mode` | 是 | `trusted`、`guest` 或 `isolated` 之一。 |
| `classification[].match.macs` | 否 | 裝置的 MAC 位址，routerd 在產生前會進行正規化。 |
| `classification[].match.ouiPrefixes` | 否 | 廠商 OUI 前綴，例如 `18:ec:e7`。 |
| `classification[].match.hostnamePatterns` | 否 | 針對觀測到的 DHCP 主機名稱的 glob 模式。 |
| `classification[].match.dhcpFingerprints` | 否 | routerd 觀測到的 DHCP 指紋標籤。 |
| `classification[].name` | 否 | 供人閱讀的裝置名稱，目前為說明用途。 |
| `classification[].ipv4Reservation` | 否 | `DHCPv4Reservation` 的名稱，寫 `aiseg2` 而非 `DHCPv4Reservation/aiseg2`。 |
| `guestServices` | 否 | 允許訪客裝置存取的路由器服務。預設為 `dhcp`、`dns`、`ntp`。可指定 `dhcp`、`dns`、`ntp`、`mdns`、`ssdp`。 |
| `guestEgressDeny` | 否 | 封鎖訪客裝置轉送目的地的 CIDR。省略時封鎖 RFC 1918 與 ULA。 |
| `guestEgressAllow` | 否 | 優先於拒絕規則的許可 CIDR。 |

預設的 `guestEgressDeny` 如下：

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `fc00::/7`

指定 `isolation.mDNSBroadcast: deny` 時，也會拒絕訪客發出的 mDNS、SSDP、NetBIOS 探索封包的轉送，防止訪客裝置透過多播或廣播探索 LAN 內的其他裝置。

許可規則在拒絕規則之前產生，適合為印表機或 captive portal 輔助伺服器等建立窄範圍的例外。

## 範例 1：最小 include 模式

將單一 MAC 位址視為訪客。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "18:ec:e7:33:12:6c"
        name: aiseg2
```

## 範例 2：多裝置 include 模式

可將多台裝置套用相同的訪客規則。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: household-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "18:ec:e7:33:12:6c"
        name: aiseg2
        ipv4Reservation: aiseg2
      - mode: guest
        match:
          macs:
            - "7c:2f:80:11:22:33"
        name: guest-phone
      - mode: guest
        match:
          macs:
            - "90:09:d0:44:55:66"
        name: smart-tv
```

## 範例 3：BYOD 用的 exclude 模式

將目標介面上的所有裝置預設視為訪客，僅將清單中的 MAC 位址視為受信任。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: byod-default-guest
  spec:
    mode: exclude
    interfaces:
      - Interface/lan
    classification:
      - mode: trusted
        match:
          macs:
            - "bc:24:11:e0:8e:3a"
        name: admin-laptop
      - mode: trusted
        match:
          macs:
            - "4e:20:15:aa:e0:67"
        name: owner-phone
```

## 範例 4：自訂拒絕與許可 CIDR

保留預設的私有目的地封鎖，同時允許單台印表機。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-with-printer
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestEgressAllow:
      - 172.18.20.10/32
    guestEgressDeny:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - fc00::/7
    classification:
      - mode: guest
        match:
          macs:
            - "7c:2f:80:11:22:33"
        name: guest-phone
```

## 範例 5：本機探索服務

預設允許訪客裝置使用 DHCP、DNS、NTP。
若路由器上有執行本機探索的 proxy 或中繼，可明確新增 `mdns` 或 `ssdp`。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: media-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestServices:
      - dhcp
      - dns
      - ntp
      - mdns
      - ssdp
    classification:
      - mode: guest
        match:
          macs:
            - "90:09:d0:44:55:66"
        name: smart-tv
```

啟用探索服務前，請先了解所公開的資訊範圍。
mDNS 與 SSDP 雖然便利，但可能揭露裝置名稱與服務資訊。

## 範例 6：IoT 隔離與固定分配

固定分配可讓排查問題更容易，也能讓 Web 管理介面的裝置清單及 DNS 記錄更清晰。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: thermostat
  spec:
    server: lan-v4
    macAddress: "02:11:22:33:44:55"
    hostname: thermostat
    ipAddress: 172.18.0.151

- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: iot-isolation
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "02:11:22:33:44:55"
        name: thermostat
        ipv4Reservation: thermostat
```

## 與 DHCPv4Reservation 的搭配

`classification[].ipv4Reservation` 用於驗證參照。
routerd 會確認指定的 `DHCPv4Reservation` 存在。
防火牆比對使用 MAC 位址，而非租約的 IP 位址。

此分離是刻意設計的：

- MAC 比對讓裝置分類在 IP 層判斷之前進行。
- IPv4 固定租約讓 DNS 與 Web 管理介面的顯示更穩定。
- 即使裝置的 IP 位址改變，訪客隔離仍會追蹤 MAC 位址。

若裝置使用隨機 MAC 位址，請登錄該 SSID 或有線區段中路由器實際觀測到的 MAC 位址。

## 確認產生的規則

產生設定，或確認執行中的 nftables 資料表：

```sh
routerd render nftables --config /usr/local/etc/routerd/router.yaml
sudo nft list table inet routerd_filter
```

確認是否有類似以下的規則：

```nft
set client_policy_guest_devices {
  type ether_addr
  elements = { 18:ec:e7:33:12:6c }
}

iifname "ens19" ether saddr @client_policy_guest_devices udp dport 53 counter accept
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 10.0.0.0/8 counter log prefix "routerd client-policy guest-devices deny " drop
```

## 從訪客裝置驗證

從訪客裝置執行以下確認：

```sh
curl -4 https://www.google.com/generate_204
curl -4 --connect-timeout 3 http://192.168.1.1/
curl -4 --connect-timeout 3 http://172.18.0.1:8080/
```

預期結果如下：

- 網際網路通訊成功。
- 私有目的地超時或失敗。
- DNS、DHCP、NTP 繼續正常運作。

在路由器側，使用 tcpdump 確認封包路徑：

```sh
sudo tcpdump -ni ens19 ether host 18:ec:e7:33:12:6c
sudo nft list chain inet routerd_filter forward
```

當私有目的地被拒絕時，產生的 `ClientPolicy` 規則計數器會增加。

## 疑難排解

### MAC 位址不相符

確認路由器所看到的 MAC 位址：

```sh
ip neigh show dev ens19
sudo tcpdump -eni ens19
```

無線裝置可能在不同 SSID 使用不同的 MAC 位址。
智慧型手機與筆電也可能使用隨機 MAC 位址。
請使用路由器在 LAN 側實際觀測到的位址，而非印在裝置上的位址。

### 訪客裝置仍能存取私有網路

請確認以下幾點：

- 策略是否參照了正確的 `Interface`。
- 封包是否確實從該介面進入。
- `routerd apply` 是否已套用最新的 nftables 資料表。
- `guestEgressAllow` 中是否包含了寬範圍的私有前綴。
- 裝置側是否有 VPN 用戶端等繞過路由器的路徑。

### 訪客裝置無法連上網際網路

`ClientPolicy` 只限制私有目的地與 self 目的地的存取。
若網際網路連線失敗，請檢查路由策略、NAT44、DS-Lite、DNS 及 IP forwarding：

```sh
routerctl status
sysctl net.ipv4.ip_forward
sudo nft list table ip routerd_nat
```

### guestServices 的作用

`guestServices` 僅控制對路由器本機服務的存取，並非允許轉送至私有子網路。
轉送的例外請透過 `guestEgressAllow` 表達。

## 安全注意事項

以 MAC 位址進行隔離是實用的方式，但並非密碼學意義上的識別。
惡意使用者若能控制裝置，即可偽造受信任的 MAC 位址。

`ClientPolicy` 適合作為家庭或小型辦公室的實用管控手段，
不應作為對抗惡意使用者的唯一防線。
更強固的設計包括：

- VLAN 或 SSID 隔離。
- WPA3 Enterprise 或 802.1X。
- 交換器的 port 隔離。
- 各裝置的專屬憑證。
- 專用的訪客 bridge 或 VRF。

即使結合上述措施，`ClientPolicy` 仍有其價值，
因為它能在 routerd 的資源模型中保留裝置分類的意圖。

## OS 支援

支援 Linux 的 nftables。

FreeBSD 的 pf 在 routerd 用於 `FirewallZone` 與 `FirewallRule` 的路由式過濾路徑中，
沒有等效的 MAC 式分類模型，因此 routerd 在 FreeBSD 上明確將 `ClientPolicy` 標示為不支援，
不會以無作用的 no-op 策略靜默套用。

未來 FreeBSD 的支援方向可能是 bridge 層級的過濾器或專用的第二層隔離資源，
但由於與路由式 pf 規則不等價，應視為獨立設計。

## 相關資源

- `FirewallZone`：將介面分配至 `trust`、`untrust`、`mgmt`。
- `FirewallPolicy`：啟用拒絕記錄等共通行為。
- `FirewallRule`：表達與 MAC 分類無關的例外。
- `DHCPv4Reservation`：為已分類的裝置提供穩定的 IPv4 位址與主機名稱。
- 從隧道自動推導的 TCP MSS clamp，也適用於防火牆區域與隧道路徑相符的訪客轉送。訪客隔離不會繞過 MSS clamp。
