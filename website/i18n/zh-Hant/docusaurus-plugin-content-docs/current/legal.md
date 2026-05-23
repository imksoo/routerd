---
title: 法務與再分發
---

# 法務與再分發

routerd 本體以 BSD 3-Clause License 分發。
完整授權條款文字位於 repository root 的 `LICENSE`。

routerd 的著作權聲明如下。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

本頁彙整再分發 routerd release archive 及 Live ISO 時的實務確認事項。本頁內容不構成法律建議。

## routerd 二進位檔

routerd 二進位檔由本 repository 的 Go source code 建置。
release 前請執行：

```sh
make third-party-licenses
```

此指令會重新產生 `THIRD_PARTY_LICENSES.md`，並列出下列資訊：

- 連結進 routerd 二進位檔的 Go 模組
- 偵測到的授權條款文字類型
- 授權條款檔案名稱
- 模組的 source URL
- Live ISO 使用的 Alpine 套件
- Alpine 套件的授權條款 metadata 與 upstream URL

目前的稽核流程會掃描 Go 模組授權條款檔案中的 GPL、LGPL、AGPL 文字。
若在被連結的 Go 模組中偵測到此類文字，請停止 release，
並確認是否需要變更 routerd 二進位檔的授權條款，或移除該相依套件。

source 檔案使用下列 SPDX 識別子：

```text
SPDX-License-Identifier: BSD-3-Clause
```

此識別子僅表示 routerd source code 的授權條款。
它不會變更隨包附帶的工具、Alpine 套件、Go 模組或其他第三方元件的授權條款。
這些元件的授權條款資訊列於 `THIRD_PARTY_LICENSES.md`。

## Release archive

Release archive 包含下列內容：

- routerd 二進位檔
- 安裝程式 script
- systemd 或 rc.d 服務範本
- 範例設定
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

再分發 release archive 時，請一併保留上述檔案。

## Live ISO

Live ISO 是彙整式發布物，組合了下列內容：

- routerd 二進位檔與 script
- Alpine Linux 基礎檔案
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、
  tcpdump 等 Alpine 套件

這些 Alpine 套件各自保留其 upstream 授權條款。部分套件採用 GPL 授權條款。
Live ISO 整體並不因此被重新授權為單一 GPL 著作物。

Live ISO 在下列路徑提供 routerd 的授權聲明：

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Alpine 套件的 source 資訊可透過 Alpine 套件儲存庫、APKBUILD 記錄，
以及 `THIRD_PARTY_LICENSES.md` 中列出的 upstream URL 查閱。

## Release 檢查清單

release 前請確認下列事項：

1. 執行 `make third-party-licenses`。
2. 確認 Go 模組 copyleft 檢查未回報 GPL、LGPL 或 AGPL 模組。
3. 確認 GPL 系列授權條款僅出現在個別分發的 Alpine 套件或其他外部工具中。
4. 執行一般的測試、schema、example、website、archive 及 Live ISO 檢查。
5. 確認 release archive 包含 `share/doc/LICENSE` 與 `share/doc/THIRD_PARTY_LICENSES.md`。
6. 確認 Live ISO 包含 `/usr/share/licenses/routerd/`。

若相依套件集有較大幅度的變動，請在建立 tag 前重新檢視本頁與已產生的授權條款清單。
