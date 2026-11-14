---
title: 变更记录
---

# 变更记录

routerd 的版本历程。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。
变更分类为“新增”“变更”“弃用”“移除”“修复”“安全”。
但版本号不采用 Semantic Versioning，而是使用日期和时间型的 `vYYYYMMDD.HHmm` 格式。
本软件仍处于 v1alpha1 阶段，版本之间可能含有破坏性改动。

## Unreleased

- 移除浏览器配置向导，改为发布包含所有资源类型YAML定义的Markdown文档。

## v20261114.0631

### Fixed

- 将空的 IPv4 地址列表视为尚未配置的 IPIP 接口，让首次启动可继续配置地址。
- Live ISO 使用现有 casper `noprompt` 选项，移除重启时等待介质移除确认的交互步骤。

## v20261114.0607

### Changed

- 简化类型化配置访问、SQLite 存储层、状态转换、事件处理、MTU 选择、SAM 输入解码及 Linux IPIP 观测，移除未使用的兼容路径及测试专用代码。
- 将 PD 租约集中至单一权威状态并统一存储字段定义，保留身份、期限及旧前缀。

### Fixed

- daemon 重启后恢复匹配的已应用配置世代。
- 配置变更时停止旧 EventRule 的待处理计时器，并在配置保存成功后清理过时状态。

[Release history](https://github.com/imksoo/routerd/releases)
