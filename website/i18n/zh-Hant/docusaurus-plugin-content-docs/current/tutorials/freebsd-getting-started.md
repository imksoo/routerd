---
title: 從 FreeBSD 開始
---

# 從 FreeBSD 開始

FreeBSD 使用與 Ubuntu 和 NixOS 相同的 routerd resource model,但主機成果物是 FreeBSD native。routerd 會生成 `rc.conf.d`、`rc.d` scripts、`pf.conf`、`dhclient.conf`、dnsmasq 設定、`mpd5.conf`,以及 DS-Lite 使用的動態 `ifconfig gif` 操作。

本教學假設 FreeBSD 14.x,並使用 `/usr/local` 作為原始碼安裝位置。參考設定請使用 `examples/freebsd-edge.yaml`。

## 1. 在開發主機建置

一般做法是在開發機上建置 routerd,再把 binaries 複製到 FreeBSD router。這樣可以讓 router 保持簡潔,不用在 edge host 上放完整 Go build 環境。

```bash
make build
```

複製 binaries:

```bash
scp bin/routerd bin/routerctl bin/routerd-* admin@freebsd-router:/tmp/
```

在 router 上安裝:

```sh
sudo install -d -m 0755 /usr/local/sbin
sudo install -m 0755 /tmp/routerd /usr/local/sbin/routerd
sudo install -m 0755 /tmp/routerctl /usr/local/sbin/routerctl
sudo install -m 0755 /tmp/routerd-* /usr/local/sbin/
```

## 2. 安裝 FreeBSD 套件

請在 YAML 中用 `Package` 宣告套件。首次 bootstrap 時,可以手動安裝同一組套件,或審閱生成的 `install-packages.sh`。

```sh
sudo pkg install -y dnsmasq bind-tools wireguard-tools tailscale strongswan mpd5
```

FreeBSD base system 已提供 `ifconfig`、`sysctl`、`service`、`sysrc`、`pfctl`、`pflog0`、`netstat`、`sockstat`、`ping`、`traceroute`。

## 3. 放置 router 設定

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

套用前,請修改 interface 名稱、address 與 secret。第一次執行時,請把管理 SSH 放在獨立 interface,或準備 hypervisor console。

## 4. 驗證並審閱生成檔案

驗證設定:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
```

把 FreeBSD 成果物生成到暫存目錄:

```sh
rm -rf /tmp/routerd-freebsd-render
routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir /tmp/routerd-freebsd-render
```

預期檔案包括:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

套用到實機前請先審閱:

```sh
less /tmp/routerd-freebsd-render/rc.conf.d-routerd
less /tmp/routerd-freebsd-render/pf.conf
less /tmp/routerd-freebsd-render/dnsmasq.conf
```

## 5. 理解 FreeBSD 主機介面

routerd 會把 resource 對應到下列 FreeBSD 元件:

| 元件 | 責任 |
| --- | --- |
| `rc.conf.d-routerd` | Interface alias、forwarding、cloned interface、static route、`pf`、`pflog`、`mpd5` enablement |
| `rc.d-*` scripts | dnsmasq、firewall logger、healthcheck、Tailscale、DHCP clients 等 routerd 管理 daemons |
| `pf.conf` | Zone filtering、service holes、NAT、firewall logging |
| `pflog0` | `routerd-firewall-logger` 的 firewall log source |
| `dnsmasq.conf` | DHCPv4、DHCPv6、DHCP relay、Router Advertisement |
| `dhclient.conf` | 被接管 uplink 的 FreeBSD DHCPv4 client 行為 |
| `mpd5.conf` | PPPoE bundle、link、authentication、MTU/MRU 與 default route 行為 |
| `ifconfig gif` | 靜態 `rc.conf` 不足時的動態 DS-Lite tunnel 套用 |

## 6. 套用

先檢查 plan:

```sh
routerd plan --config /usr/local/etc/routerd/router.yaml
```

生成檔案與 plan 都符合預期後再套用:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml
```

routerd 會在載入 `pf.conf` 前用 `pfctl -nf` 驗證。重新啟動 dnsmasq 前,也會用 `dnsmasq --test` 驗證設定。

## 7. 檢查狀態與日誌

查看 routerd 狀態:

```sh
routerctl status
routerctl events --limit 20
```

追蹤系統日誌:

```sh
tail -f /var/log/routerd.log
```

查看 pf state:

```sh
sudo pfctl -ss -v
```

透過 `pflog0` 查看 firewall log:

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

啟用 `FirewallLog` 後,routerd 也會把 `pflog0` 條目匯入 firewall log store,供 `routerctl` 與 Web Console 使用。

## 下一步

接著請在 platform matrix 中確認 OS 差異,並在英文或日文文件中查看 WAN-side services 與 basic firewall 教學。
