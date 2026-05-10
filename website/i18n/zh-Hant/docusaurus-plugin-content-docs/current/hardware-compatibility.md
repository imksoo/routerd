---
title: 硬體相容性
---

# 硬體相容性

routerd 可以在支援的 OS 上執行。實務上需要確認的是網路介面、CPU、
記憶體與儲存裝置耐久性是否足以承擔路由器用途。

## 建議類型

| 類型 | 適合用途 | 備註 |
| --- | --- | --- |
| Intel NUC | lab router | 多數機型可靠，但常只有一個 Ethernet port。USB Ethernet 與 VLAN trunk 需要先驗證。 |
| Intel N100 mini PC | 家用 router | 每瓦效能佳。建議選擇 Intel i226/i225 NIC 與散熱良好的機型。 |
| Raspberry Pi 5 | edge 或 demo router | 需要穩定電源與支援良好的 USB/NVMe 儲存。吞吐量取決於轉接器。 |

## 候選硬體

這份清單是起點。除非狀態標示為「已驗證」，否則請把它視為預期適合。
正式使用前，請驗證 NIC、MTU 與重新開機後的收斂。

| 硬體 | 預期用途 | 狀態 | 備註 |
| --- | --- | --- | --- |
| 搭配 USB Ethernet 的 Intel NUC | Proxmox lab router、live ISO demo | 預期可用 | 建議使用已知穩定的 USB Ethernet。測試時保留獨立管理路徑。 |
| N100 4-port 2.5GbE mini PC | 家用 router、DS-Lite、PPPoE fallback、VPN overlay | 預期可用 | 適合作為 diskless routerd appliance 的第一台機器。 |
| N100 6-port 2.5GbE mini PC | 多 LAN、guest network、management 分離 | 預期可用 | 適合把 WAN、LAN、guest、management 分到實體 port。 |
| Raspberry Pi 5 搭配 USB 或 PCIe NIC | demo、edge router、省電 lab | 預期可用 | 請使用高品質電源。吞吐量高度依賴 NIC 與儲存路徑。 |
| 搭載 Intel NIC 的舊 thin client | 備援 router、lab node | 預期可用 | 適合測試。請確認 AES、散熱與儲存健康狀態。 |
| Proxmox VM | SDN/VNET routing、整合測試 | lab 已驗證 | 同一份 resource 之後可以移到實體 mini PC。 |

## Live ISO 與 USB persistence

Live ISO 同時用於快速試用與 diskless 運作。

- 從 ISO 開機。
- 在畫面或 serial console 執行文字 wizard。
- 將 `router.yaml` 與選定狀態存到 USB。
- 日誌先緩存在 tmpfs。
- 每天一次把壓縮日誌與狀態 snapshot 寫入 USB。

沒有 USB persistence 時，live ISO 是一次性的 demo router。
有 USB persistence 時，同一台 mini PC 可以用保存的 router 設定重新開機。
