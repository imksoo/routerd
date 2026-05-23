---
title: 法律与再分发
---

# 法律与再分发

routerd 本体以 BSD 3-Clause License 分发。
完整许可证文本位于 repository root 的 `LICENSE`。

routerd 的版权声明如下。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

本页汇整再分发 routerd release archive 及 Live ISO 时的实际确认事项。本页内容不构成法律建议。

## routerd 二进制文件

routerd 二进制文件由本 repository 的 Go source code 构建。
发布前请执行：

```sh
make third-party-licenses
```

此命令会重新生成 `THIRD_PARTY_LICENSES.md`，并列出下列信息：

- 链接进 routerd 二进制文件的 Go 模块
- 检测到的许可证文本类型
- 许可证文件名称
- 模块的 source URL
- Live ISO 使用的 Alpine 软件包
- Alpine 软件包的许可证 metadata 与 upstream URL

目前的审计流程会扫描 Go 模块许可证文件中的 GPL、LGPL、AGPL 文本。
若在被链接的 Go 模块中检测到此类文本，请停止发布，
并确认是否需要变更 routerd 二进制文件的许可证，或移除该依赖软件包。

source 文件使用下列 SPDX 标识符：

```text
SPDX-License-Identifier: BSD-3-Clause
```

此标识符仅表示 routerd source code 的许可证。
它不会变更随包附带的工具、Alpine 软件包、Go 模块或其他第三方组件的许可证。
这些组件的许可证信息列于 `THIRD_PARTY_LICENSES.md`。

## Release archive

Release archive 包含下列内容：

- routerd 二进制文件
- 安装程序 script
- systemd 或 rc.d 服务模板
- 示例配置
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

再分发 release archive 时，请一并保留上述文件。

## Live ISO

Live ISO 是汇总式发布物，组合了下列内容：

- routerd 二进制文件与 script
- Alpine Linux 基础文件
- dnsmasq、nftables、WireGuard tools、ppp、iproute2、chrony、
  tcpdump 等 Alpine 软件包

这些 Alpine 软件包各自保留其 upstream 许可证。部分软件包采用 GPL 许可证。
Live ISO 整体并不因此被重新授权为单一 GPL 著作物。

Live ISO 在下列路径提供 routerd 的许可声明：

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Alpine 软件包的 source 信息可通过 Alpine 软件包仓库、APKBUILD 记录，
以及 `THIRD_PARTY_LICENSES.md` 中列出的 upstream URL 查阅。

## Release 检查清单

发布前请确认下列事项：

1. 执行 `make third-party-licenses`。
2. 确认 Go 模块 copyleft 检查未报告 GPL、LGPL 或 AGPL 模块。
3. 确认 GPL 系列许可证仅出现在单独分发的 Alpine 软件包或其他外部工具中。
4. 执行常规的测试、schema、example、website、archive 及 Live ISO 检查。
5. 确认 release archive 包含 `share/doc/LICENSE` 与 `share/doc/THIRD_PARTY_LICENSES.md`。
6. 确认 Live ISO 包含 `/usr/share/licenses/routerd/`。

若依赖软件包集合有较大幅度的变动，请在创建 tag 前重新审查本页与已生成的许可证清单。
