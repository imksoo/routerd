---
title: 變更記錄
---

# 變更記錄

routerd 的版本歷程。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-TW/)。
變更分類為「新增」「變更」「棄用」「移除」「修正」「安全性」。
但版號不採用 Semantic Versioning，而是使用日期與時間型的 `vYYYYMMDD.HHmm` 格式。
本軟體仍在 v1alpha1 階段，版本之間可能含有破壞性變更。

## Unreleased

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
