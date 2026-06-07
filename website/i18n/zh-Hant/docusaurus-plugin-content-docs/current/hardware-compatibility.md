---
title: 硬體相容性
---

# 硬體相容性

![Diagram showing hardware compatibility decisions from platform class selection through CPU, memory, NIC, storage, live ISO persistence, and validation checklist](/img/diagrams/hardware-compatibility.png)

routerd 可在具備必要核心功能與使用者空間功能的支援 OS 上運行。
實務上的重點在於，作為路由器使用時，網路介面、CPU、記憶體與儲存裝置耐久性是否充足。

## 建議類型

| 類型 | 適性 | 備註 |
| --- | --- | --- |
| Intel NUC | 適合實驗室路由器 | 可靠性通常較高。但多數機型只有一個 Ethernet 埠，使用 USB Ethernet 或 VLAN trunk 時需謹慎評估。 |
| Intel N100 mini PC | 適合家庭路由器 | 每瓦效能優秀。建議選擇搭載 Intel i226/i225 NIC 並具備良好散熱的機型。 |
| Raspberry Pi 5 | 適合 edge 或展示用途 | 需要高品質電源及相容性良好的 USB/NVMe 儲存裝置。吞吐量取決於轉接器。 |

## 候選硬體

以下清單僅供參考。
狀態未標示為「已驗證」者，請視為預期適用。
正式作為路由器使用前，請先確認 NIC、MTU 及重新開機後的收斂狀況。

| 硬體 | 預期用途 | 狀態 | 備註 |
| --- | --- | --- | --- |
| 搭配 USB Ethernet 的 Intel NUC | Proxmox 實驗室路由器、live ISO 展示 | 預期可用 | 建議選用有實績的 USB Ethernet 轉接器。測試時請將管理路徑分隔至獨立的 VLAN 或介面。 |
| N100 4 埠 2.5GbE mini PC | 家庭路由器、DS-Lite、PPPoE 故障切換、VPN overlay | 預期可用 | 無磁碟 routerd 設備的首選。請確認 Intel i226/i225 NIC 及散熱狀況。 |
| N100 6 埠 2.5GbE mini PC | 多 LAN、訪客網路、管理路徑分離 | 預期可用 | 適合以實體埠分隔 WAN、LAN、訪客、管理網路的場景。同時請確認 BIOS 的電源恢復設定。 |
| 搭配 USB 或 PCIe NIC 的 Raspberry Pi 5 | 展示、edge 路由器、省電實驗室 | 預期可用 | 需要強力電源。吞吐量高度依賴 NIC 與儲存路徑。 |
| 搭載 Intel NIC 的舊型 thin client | 備援路由器、實驗室節點 | 預期可用 | 適合測試使用。請確認 AES 支援、散熱及儲存裝置的健康狀態。 |
| Proxmox 上的虛擬機 | SDN/VNET 路由、類 CI 實驗室、整合測試 | 實驗室已驗證 | 同一份資源日後可遷移至實體 mini PC，這正是 routerd 的優勢所在。 |

## CPU 與記憶體

家庭或小型辦公室環境的參考標準如下。

- 基本的路由控制、DHCP、DNS、NAT 及 Web 管理介面，2 核心即已足夠。
- 若使用加密 DNS、OpenTelemetry 或日誌保存，4 核心較為合適。
- 1 GiB RAM 為實用下限。
- 使用 live ISO 與日誌緩衝區時，建議 2 GiB 以上。

## 網路介面

建議至少配備 2 個實體介面。

- WAN 或 untrust
- LAN 或 trust

若有第 3 個管理介面，防火牆變更的測試將更為安全，可將 SSH 與 Web 管理介面從 WAN/LAN 策略中分離。

也可以在單一 NIC 上進行 VLAN 路由設定，但初始設定時遺失管理路徑的風險較高。套用前請務必先確認 plan 的結果。

## 儲存裝置

一般安裝建議使用 SSD 或 NVMe。
無磁碟 mini PC 可搭配 USB 持久化的 live ISO 使用。

- 將設定儲存至 USB 裝置。
- 日誌暫存於 `/run/routerd/logs` 的 tmpfs。
- 每日一次，將壓縮日誌與狀態快照寫入 USB。

這樣可以減少對低價快閃儲存媒體的寫入次數。

## Live ISO 與 USB 持久化

Live ISO 同時適用於短期評估與無磁碟運作。

- 從 ISO 開機。
- 在螢幕或序列 console 上執行文字精靈。
- 將 `router.yaml` 與選定狀態儲存至 USB。
- 日誌暫存於 tmpfs。
- 每日一次，將壓縮日誌與狀態快照寫入 USB。

未使用 USB 持久化時，live ISO 作為臨時展示路由器運作。
使用 USB 持久化時，同一台 mini PC 可以用儲存的設定重新開機並繼續服務。

## NIC 備註

| NIC 類型 | 建議 |
| --- | --- |
| Intel i210/i211 | 穩健可靠的選擇。 |
| Intel i225/i226 | 2.5GbE 的良好選擇。請保持 firmware 與 OS 驅動程式在最新版本。 |
| Realtek 2.5GbE | 通常可正常運作，但正式環境使用前請先進行負載測試。 |
| USB Ethernet | 展示或 NUC 上很方便。正式路由器請避免使用來路不明的轉接器。 |

## 平台備註

Ubuntu Server 為主要支援對象。
NixOS 與 FreeBSD 透過平台專屬的產生器（renderer）與服務整合提供支援。
在 Linux 以外的平台上依賴特定功能時，請參閱[平台](./platforms)頁面確認。

## 驗證清單

1. 啟動目標 OS 或 live ISO。
2. 確認所有 NIC 名稱穩定不變。
3. 執行 `routerctl validate` 與 `routerctl plan`。
4. 若可行，請在管理路徑分離後再套用。
5. 確認 DHCP、DNS、NAT、防火牆及路由策略正常運作。
6. 執行吞吐量測試。
7. 確認 CPU 溫度與封包遺失狀況。
8. 重新開機後，確認無需手動指令即可自動收斂。
