---
title: 法律与再分发
---

# 法律与再分发

routerd 本体以 BSD 3-Clause License 分发。完整许可证文本位于仓库根目录的 `LICENSE`。

routerd 的版权声明如下。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

本页总结再分发 routerd release archive 和 routerd live ISO 时的实际确认事项。它不是法律建议。

## routerd 二进制文件

routerd 二进制文件由本仓库中的 Go source code 构建。发布前请运行：

```sh
make third-party-licenses
```

该命令会重新生成 `THIRD_PARTY_LICENSES.md`，其中列出：

- 链接进 routerd 二进制文件的 Go module
- 检测到的 license text 类型
- license file 名称
- module source URL
- live ISO 使用的 Alpine package
- Alpine package 的 license metadata 和 upstream URL

当前 audit path 会检查 Go module license file 中的 GPL、LGPL 和 AGPL 文本。如果这类文本出现在被链接的 Go module 中，请停止发布，并确认是否需要改变 routerd 二进制文件的许可证或移除该依赖。

源文件使用以下 SPDX 标识符。

```text
SPDX-License-Identifier: BSD-3-Clause
```

这些头部只表示 routerd 源代码许可证。它们不会改变随包提供的工具、Alpine package、Go module 或其他第三方组件的许可证。这些组件列在 `THIRD_PARTY_LICENSES.md` 中。

## Release archive

Release archive 包含：

- routerd 二进制文件
- installer script
- systemd 或 rc.d service template
- sample configuration
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

再分发 release archive 时，请保留这些文件。

## Live ISO

live ISO 是 aggregate distribution。它组合了：

- routerd 二进制文件和 script
- Alpine Linux base file
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、tcpdump 等 Alpine package

这些 Alpine package 保留各自的 upstream license。其中一部分使用 GPL license。live ISO 不会因此作为一个整体重新许可为单一 GPL work。

live ISO 在以下位置包含 routerd notices。

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Alpine package 的 source 信息可以通过 Alpine package repositories、APKBUILD records，以及 `THIRD_PARTY_LICENSES.md` 中列出的 upstream URL 查阅。

## Release checklist

发布前请确认：

1. 运行 `make third-party-licenses`。
2. 确认 Go module copyleft check 没有报告 GPL、LGPL 或 AGPL module。
3. 确认 GPL-family license 只出现在单独分发的 Alpine package 或其他外部工具中。
4. 运行通常的 test、schema、example、website、archive 和 live ISO checks。
5. 确认 release archive 包含 `share/doc/LICENSE` 和 `share/doc/THIRD_PARTY_LICENSES.md`。
6. 确认 live ISO 包含 `/usr/share/licenses/routerd/`。

如果依赖集合有较大变化，请在创建 tag 前重新检查本页和生成的 license inventory。
