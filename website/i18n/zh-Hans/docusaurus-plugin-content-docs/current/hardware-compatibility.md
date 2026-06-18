---
title: 硬件兼容性
---

# 硬件兼容性

![Diagram showing hardware compatibility decisions from platform class selection through CPU, memory, NIC, storage, live ISO persistence, and validation checklist](/img/diagrams/hardware-compatibility.png)

routerd 可在具备必要内核功能与用户空间功能的支持 OS 上运行。
实际上的重点在于，作为路由器使用时，网络接口、CPU、内存与存储装置耐久性是否充足。

## 建议类型

| 类型 | 适性 | 备注 |
| --- | --- | --- |
| Intel NUC | 适合实验室路由器 | 可靠性通常较高。但多数机型只有一个 Ethernet 端口，使用 USB Ethernet 或 VLAN trunk 时需谨慎评估。 |
| Intel N100 mini PC | 适合家庭路由器 | 每瓦性能优秀。建议选择搭载 Intel i226/i225 NIC 并具备良好散热的机型。 |
| Raspberry Pi 5 | 适合 edge 或展示用途 | 需要高质量电源及兼容性良好的 USB/NVMe 存储装置。吞吐量取决于转接器。 |

## 候选硬件

以下清单仅供参考。
状态未标注为「已验证」者，请视为预期适用。
正式作为路由器使用前，请先确认 NIC、MTU 及重新开机后的收敛状况。

| 硬件 | 预期用途 | 状态 | 备注 |
| --- | --- | --- | --- |
| 搭配 USB Ethernet 的 Intel NUC | Proxmox 实验室路由器、live ISO 展示 | 预期可用 | 建议选用有实绩的 USB Ethernet 转接器。测试时请将管理路径分隔至独立的 VLAN 或接口。 |
| N100 4 端口 2.5GbE mini PC | 家庭路由器、DS-Lite、PPPoE 故障切换、VPN overlay | 预期可用 | 无磁盘 routerd 设备的首选。请确认 Intel i226/i225 NIC 及散热状况。 |
| N100 6 端口 2.5GbE mini PC | 多 LAN、访客网络、管理路径分离 | 预期可用 | 适合以实体端口分隔 WAN、LAN、访客、管理网络的场景。同时请确认 BIOS 的电源恢复设置。 |
| 搭配 USB 或 PCIe NIC 的 Raspberry Pi 5 | 展示、edge 路由器、省电实验室 | 预期可用 | 需要强力电源。吞吐量高度依赖 NIC 与存储路径。 |
| 搭载 Intel NIC 的旧型 thin client | 备援路由器、实验室节点 | 预期可用 | 适合测试使用。请确认 AES 支持、散热及存储装置的健康状态。 |
| Proxmox 上的虚拟机 | SDN/VNET 路由、类 CI 实验室、集成测试 | 实验室已验证 | 同一份资源日后可迁移至实体 mini PC，这正是 routerd 的优势所在。 |

## CPU 与内存

家庭或小型办公室环境的参考标准如下。

- 基本的路由控制、DHCP、DNS、NAT 及 Web 管理界面，2 核心即已足够。
- 若使用加密 DNS、OpenTelemetry 或日志保存，4 核心较为合适。
- 1 GiB RAM 为实用下限。
- 使用 live ISO 与日志缓冲区时，建议 2 GiB 以上。

## 网络接口

建议至少配备 2 个实体接口。

- WAN 或 untrust
- LAN 或 trust

若有第 3 个管理接口，防火墙变更的测试将更为安全，可将 SSH 与 Web 管理界面从 WAN/LAN 策略中分离。

也可以在单一 NIC 上进行 VLAN 路由配置，但初始配置时丢失管理路径的风险较高。应用前请务必先确认 plan 的结果。

## 存储装置

一般安装建议使用 SSD 或 NVMe。
无磁盘 mini PC 可搭配 USB 持久化的 live ISO 使用。

- 将配置存储至 USB 装置。
- 日志暂存于 `/run/routerd/logs` 的 tmpfs。
- 每日一次，将压缩日志与状态快照写入 USB。

这样可以减少对低价闪存存储媒体的写入次数。

## Live ISO 与 USB 持久化

Live ISO 同时适用于短期评估与无磁盘运作。

- 从 ISO 开机。
- 在屏幕或串行 console 上执行文字向导。
- 将 `router.yaml` 与选定状态存储至 USB。
- 日志暂存于 tmpfs。
- 每日一次，将压缩日志与状态快照写入 USB。

未使用 USB 持久化时，live ISO 作为临时展示路由器运作。
使用 USB 持久化时，同一台 mini PC 可以用存储的配置重新开机并继续服务。

## NIC 备注

| NIC 类型 | 建议 |
| --- | --- |
| Intel i210/i211 | 稳健可靠的选择。 |
| Intel i225/i226 | 2.5GbE 的良好选择。请保持 firmware 与 OS 驱动程序在最新版本。 |
| Realtek 2.5GbE | 通常可正常运作，但正式环境使用前请先进行负载测试。 |
| USB Ethernet | 展示或 NUC 上很方便。正式路由器请避免使用来路不明的转接器。 |

## 平台备注

Ubuntu Server 为主要支持对象。
FreeBSD 通过平台专属的生成器（renderer）与服务整合提供支持。
在 Linux 以外的平台上依赖特定功能时，请参阅[平台](./platforms)页面确认。

## 验证清单

1. 启动目标 OS 或 live ISO。
2. 确认所有 NIC 名称稳定不变。
3. 执行 `routerctl validate` 与 `routerctl plan`。
4. 若可行，请在管理路径分离后再应用。
5. 确认 DHCP、DNS、NAT、防火墙及路由策略正常运作。
6. 执行吞吐量测试。
7. 确认 CPU 温度与丢包状况。
8. 重新开机后，确认无需手动命令即可自动收敛。
