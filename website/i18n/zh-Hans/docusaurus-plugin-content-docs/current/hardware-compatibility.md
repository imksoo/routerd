---
title: 硬件兼容性
---

# 硬件兼容性

routerd 可以在受支持的 OS 上运行。实际需要确认的是网络接口、CPU、
内存和存储耐久性是否足以承担路由器用途。

## 推荐类型

| 类型 | 适合用途 | 备注 |
| --- | --- | --- |
| Intel NUC | lab router | 多数机型可靠，但常只有一个 Ethernet port。USB Ethernet 和 VLAN trunk 需要先验证。 |
| Intel N100 mini PC | 家用 router | 每瓦性能好。建议选择 Intel i226/i225 NIC 和散热良好的机型。 |
| Raspberry Pi 5 | edge 或 demo router | 需要稳定电源和支持良好的 USB/NVMe 存储。吞吐量取决于转接器。 |

## 候选硬件

这份清单是起点。除非状态标为“已验证”，否则请把它视为预期适合。
正式使用前，请验证 NIC、MTU 和重启后的收敛。

| 硬件 | 预期用途 | 状态 | 备注 |
| --- | --- | --- | --- |
| 搭配 USB Ethernet 的 Intel NUC | Proxmox lab router、live ISO demo | 预期可用 | 建议使用已知稳定的 USB Ethernet。测试时保留独立管理路径。 |
| N100 4-port 2.5GbE mini PC | 家用 router、DS-Lite、PPPoE fallback、VPN overlay | 预期可用 | 适合作为 diskless routerd appliance 的第一台机器。 |
| N100 6-port 2.5GbE mini PC | 多 LAN、guest network、management 分离 | 预期可用 | 适合把 WAN、LAN、guest、management 分到实体 port。 |
| Raspberry Pi 5 搭配 USB 或 PCIe NIC | demo、edge router、低功耗 lab | 预期可用 | 请使用高质量电源。吞吐量高度依赖 NIC 和存储路径。 |
| 搭载 Intel NIC 的旧 thin client | 备用 router、lab node | 预期可用 | 适合测试。请确认 AES、散热和存储健康状态。 |
| Proxmox VM | SDN/VNET routing、集成测试 | lab 已验证 | 同一份 resource 之后可以移到实体 mini PC。 |

## Live ISO 与 USB persistence

Live ISO 同时用于快速试用和 diskless 运行。

- 从 ISO 启动。
- 在画面或 serial console 执行文本 wizard。
- 将 `router.yaml` 和选定状态保存到 USB。
- 日志先缓存在 tmpfs。
- 每天一次把压缩日志和状态 snapshot 写入 USB。

没有 USB persistence 时，live ISO 是一次性的 demo router。
有 USB persistence 时，同一台 mini PC 可以用保存的 router 设置重启。
