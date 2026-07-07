---
title: 安裝
sidebar_position: 1
---

# 安裝

![從 release archive install routerd，安裝 dependency 與 service template，preserve config/state，並執行 validate-plan-dry-run 的流程](/img/diagrams/tutorial-install.png)

routerd 從 release 封存檔安裝。
路由器主機上不需要 Go 或 Makefile。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 主機請使用 `routerd-linux-arm64.tar.gz`。

FreeBSD 請取得 `routerd-freebsd-amd64.tar.gz`，並執行相同的
`./install.sh`。
FreeBSD arm64 主機請使用 `routerd-freebsd-arm64.tar.gz`。
若要固定至特定版本，請使用 release 頁面上附有版本號的封存檔。

Linux 封存檔包含靜態連結的 routerd 執行檔
（`CGO_ENABLED=0`）。
不依賴路由器主機的 glibc 版本。

安裝程式將執行以下步驟：

- 透過對應的套件管理員安裝執行時期套件。
- 將執行檔放置至 `/usr/local/sbin`。
- 放置 systemd 或 rc.d 的服務範本。
- 建立 `/usr/local/etc/routerd/router.yaml.sample`。
- 保留現有的 `/usr/local/etc/routerd/router.yaml`。
- 保留 `/var/lib/routerd` 或 `/var/db/routerd` 的狀態。
- 若有唯讀狀態 socket，則執行 `routerctl get status`。

常用選項：

```sh
./install.sh --list-deps
sudo ./install.sh --no-install-deps
sudo ./install.sh --deps-only
sudo ./install.sh --with-tailscale
sudo ./install.sh --dry-run
```

安裝後，建立設定檔並進行驗證。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

確認管理路徑仍可存取後再正式套用。

```sh
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

各 OS 的套件清單、升級、解除安裝，以及開發者適用的
release 步驟，請參閱[安裝與升級](../install-and-upgrade.md)。

若要在不寫入磁碟的情況下試用，請啟動 `routerd-live.iso`。
以 root 登入後，相同的 `install.sh configure` 精靈將會啟動。
亦支援 Proxmox VE 的 `qm terminal` 序列主控台。
在精靈中選擇 USB 永久保存，即可將 Live ISO 作為無磁碟的永久路由器使用。
若不選擇 USB 永久保存，ISO 將作為臨時示範運行，重新啟動後設定將消失。
