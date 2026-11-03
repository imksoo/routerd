---
title: 安裝與升級
---

# 安裝與升級

routerd 1.5 提案版以全新安裝的 OS 為前提，使用 Debian/Ubuntu `.deb` 或 FreeBSD `.pkg`。
目前仍在開發；舊 stable release 的封存檔不是 1.5 套件，也尚未公布公共 apt/pkg
儲存庫。取得符合目標版本、OS 與 CPU 的套件，並先確認檢查碼。

```sh
# Debian/Ubuntu
sudo apt install ./routerd_<version>_amd64.deb
# FreeBSD 14
sudo pkg -o HANDLE_RC_SCRIPTS=true install ./routerd-<version>.pkg
```

請替換為實際檔名；arm64 主機使用 arm64 套件。
首次安裝僅放置檔案與服務定義，不啟動 daemon，也不套用範例設定。
Linux 設定為 `/etc/routerd/router.yaml`，持久狀態為 `/var/lib/routerd`；
FreeBSD 設定為 `/usr/local/etc/routerd/router.yaml`，持久狀態為 `/var/db/routerd`。
範例位於 Linux `/usr/share/doc/routerd/examples` 或
FreeBSD `/usr/local/share/doc/routerd/examples`，並非任何環境皆可安全使用的預設設定。

確認設定並保留獨立管理路徑後，先執行 `routerd validate --config <實際路徑>`。
Linux 使用 `sudo systemctl enable --now routerd.service`；FreeBSD 使用
`sudo sysrc routerd_enable=YES` 與 `sudo service routerd start`。
接著從獨立用戶端檢查實際通訊；安裝成功不代表路由功能已驗證。

以相同 apt/pkg 命令安裝新版套件，更新時保留設定與持久狀態。
Debian 更新會停止舊的自有 daemon/helper，置換檔案，再恢復先前運作的服務。
FreeBSD 更新須使用上述 `HANDLE_RC_SCRIPTS=true`，並為運作中的路由器設定
`routerd_enable=YES`；pkg 在 hook 清理後啟動已啟用的 rc.d 服務。因此，FreeBSD
服務即使更新前已停止，只要啟用也可能啟動。此選項只作用於當次 pkg 命令，不修改全域設定。
不使用此服務策略可能導致更新後服務保持停止。更新可能造成通訊中斷。
無法確認停止成功時拒絕置換。不要用手動複製覆寫套件管理的檔案。
不提供 1.0、`make install`、手動複製或封存檔安裝的遷移與接管路徑。

移除使用 `sudo apt remove routerd` 或 `sudo pkg delete routerd`。
移除後保留設定與資料庫，包含 Debian purge。撤除路由器前應先撤回設定資源並
核驗實際網路狀態；移除本身不保證撤回全部網路效果。

完整路徑、更新邊界與可重複的測試方法見[英文導入指南](/docs/install-and-upgrade)。
發布 workflow 的定義不等於已發布，也不等於所有 CPU 與全部路由功能已通過驗證。
