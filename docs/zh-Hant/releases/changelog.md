---
title: 變更記錄
---

# 變更記錄

routerd 的版本歷程。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-TW/)。
變更分類為「新增」「變更」「棄用」「移除」「修正」「安全性」。
但版號不採用 Semantic Versioning，而是使用日期與時間型的 `vYYYYMMDD.HHmm` 格式。
本軟體仍在 v1alpha1 階段，版本之間可能含有破壞性變更。

## Unreleased

- DHCP 租約刪除後仍可正向及反向解析固定位址保留的主機名稱。

## v20261119.1122

- DNS 快取回應傳回剩餘 TTL。BGP 同步一次讀取所有本機 IPv4 位址。連線歷史保留未納入明細樣本的活動連線。主要 Debian 套件改用 xz 壓縮。

## v20261118.1332

- 修正動態 BGP 對等節點的條件判斷，統一資源類型與日誌路徑的解析。診斷以唯讀方式開啟狀態資料庫。簡化事件歷史、定期觀測及封裝流程，改用 esbuild 建置 Web Console。

## v20261117.0407

- 將主單元擁有權統一到 Debian 套件，以服務操作取代用戶端程序監督，固定產生單元的權限，讓 DHCP fingerprint watcher 隨 dnsmasq 執行，並將 routerd 服務歸入 `routerd.slice`。移除 `render systemd-service`；主程序 Telemetry 變更需要重新啟動。移除套件時保留 drop-in 與持久資料。

## v20261117.0008

- 修正systemd保留不存在的routerd輔助單元資訊時，Debian套件更新或重新安裝失敗的問題。

## v20261116.2323

- 將Linux安裝與更新統一為Debian套件，保留既有設定，並在套件安裝時管理服務的停止與啟動。

## v20261115.1721

- 移除瀏覽器設定精靈，改為發布包含所有資源種類YAML定義的Markdown文件。

## v20261114.0631

### Fixed

- 將空的 IPv4 位址清單視為尚未設定的 IPIP 介面，讓首次啟動可繼續配置位址。
- Live ISO 採用既有 casper `noprompt` 選項，移除重新啟動時等待媒體移除確認的互動步驟。

## v20261114.0607

### Changed

- 簡化型別化設定存取、SQLite 儲存層、狀態轉換、事件處理、MTU 選擇、SAM 輸入解碼與 Linux IPIP 觀測，移除未使用的相容路徑及測試專用程式碼。
- 將 PD 租約集中至單一權威狀態並統一儲存欄位定義，保留身分、期限及舊前綴。

### Fixed

- daemon 重新啟動後恢復相符的已套用設定世代。
- 設定變更時停止舊 EventRule 的待處理計時器，並於設定儲存成功後清理過時狀態。

[Release history](https://github.com/imksoo/routerd/releases)
