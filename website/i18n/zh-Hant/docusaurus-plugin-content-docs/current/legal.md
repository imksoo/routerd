---
title: 法律與再分發
---

# 法律與再分發

routerd 本體以 BSD 3-Clause License 分發。完整授權條款文字位於 repository root 的 `LICENSE`。

routerd 的著作權聲明如下。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

本頁總結再分發 routerd release archive 和 routerd live ISO 時的實務確認事項。它不是法律建議。

## routerd 二進位檔

routerd 二進位檔由本 repository 中的 Go source code 建置。release 前請執行：

```sh
make third-party-licenses
```

該命令會重新產生 `THIRD_PARTY_LICENSES.md`，其中列出：

- 連結進 routerd 二進位檔的 Go module
- 偵測到的 license text 類型
- license file 名稱
- module source URL
- live ISO 使用的 Alpine package
- Alpine package 的 license metadata 和 upstream URL

目前的 audit path 會檢查 Go module license file 中的 GPL、LGPL 和 AGPL 文字。如果這類文字出現在被連結的 Go module 中，請停止 release，並確認是否需要改變 routerd 二進位檔的 license 或移除該 dependency。

source file 使用以下 SPDX 識別子。

```text
SPDX-License-Identifier: BSD-3-Clause
```

這些 header 只表示 routerd source license。它們不會改變隨包提供的工具、Alpine package、Go module 或其他第三方 component 的 license。這些 component 列在 `THIRD_PARTY_LICENSES.md` 中。

## Release archive

Release archive 包含：

- routerd 二進位檔
- installer script
- systemd 或 rc.d service template
- sample configuration
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

再分發 release archive 時，請保留這些檔案。

## Live ISO

live ISO 是 aggregate distribution。它組合了：

- routerd 二進位檔和 script
- Alpine Linux base file
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、tcpdump 等 Alpine package

這些 Alpine package 保留各自的 upstream license。其中一部分使用 GPL license。live ISO 不會因此作為一個整體重新授權為單一 GPL work。

live ISO 在以下位置包含 routerd notices。

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Alpine package 的 source 資訊可以透過 Alpine package repositories、APKBUILD records，以及 `THIRD_PARTY_LICENSES.md` 中列出的 upstream URL 查閱。

## Release checklist

release 前請確認：

1. 執行 `make third-party-licenses`。
2. 確認 Go module copyleft check 沒有報告 GPL、LGPL 或 AGPL module。
3. 確認 GPL-family license 只出現在單獨分發的 Alpine package 或其他外部工具中。
4. 執行通常的 test、schema、example、website、archive 和 live ISO checks。
5. 確認 release archive 包含 `share/doc/LICENSE` 和 `share/doc/THIRD_PARTY_LICENSES.md`。
6. 確認 live ISO 包含 `/usr/share/licenses/routerd/`。

如果 dependency set 有較大變化，請在建立 tag 前重新檢查本頁和產生的 license inventory。
