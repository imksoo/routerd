# ADR 0015: hub/spoke bootstrap 的 WireGuard peer enrollment

## 狀態

提議 — 2026-06-09。

相關 issue: #377。

## 背景

`WireGuardInterface.spec.peersFrom` 已經可以從共享的 `SAMNodeSet` 衍生 WireGuard
peer。當每個 router 都已經持有可信的 node registry 時，這可以減少大部分靜態 peer 重複。

但這還不能完全解決 hub/spoke bootstrap。在 Route Reflector 或 spine 部署中，leaf router
通常主動連線固定的 RR/spine endpoint。RR/spine 側仍然必須在 kernel 接受 peer 前知道每個
leaf 的 public key、allowed IP，以及可選 endpoint。隨著 `pve-rt` 這類 leaf 增加，RR/spine
source of truth 仍會產生維護負擔。

初次接觸不能使用目標 WireGuard tunnel。WireGuard 會在應用協定執行前丟棄未知 peer，因此
enrollment 必須使用獨立 bootstrap transport，例如 management address、underlay listener
或另一個預先建立的 control channel。

## 決策

為 hub/spoke 部署增加一個可選的 WireGuard peer enrollment flow。

RR/spine router 可以在顯式配置的 non-WireGuard listen address 和 port 上公開 enrollment
endpoint。leaf 提交 node identity 和 WireGuard peer material，RR/spine 根據 local policy
和期望 topology 驗證後才啟用該 peer。

enrollment record 應包含：

- `nodeRef` 與目標 WireGuard interface；
- WireGuard `publicKey`；
- leaf 有穩定 endpoint 時的 endpoint 或 listen port；
- 請求的 `allowedIPs` 和/或 `samEndpoint`；
- 用於冪等 retry 和 stale write 檢測的 nonce 或 generation。

已核准的 registration 存為 dynamic config，而不是隱藏在 config graph 之外的臨時 runtime
state。effective config path 再將這些記錄轉換為普通 WireGuard peer input，可以是產生的
`WireGuardPeer` resource，也可以是既有 `WireGuardInterface.spec.peersFrom` 可以消費的 entry。
靜態 `WireGuardPeer` resource 繼續按名稱覆蓋產生 peer，保留緊急 override 能力。

leaf 的靜態 bootstrap config 保持很小：自身 private key、RR/spine public key 和固定 endpoint、
以及 enrollment credential。核准和啟用由 RR/spine 負責。

## 驗證和安全

enrollment 必須 fail closed。只有所有配置的檢查都通過時才接受請求。

- enrollment endpoint 預設關閉，並且只綁定到配置的 address。
- 使用 bearer token、mTLS client identity 或簽章 registration payload 等顯式機制認證請求。
- 請求的 `nodeRef` 必須被 policy 允許；配置時，還必須存在於期望的 `SAMNodeSet` 中。
- 請求的 `allowedIPs` 與 `samEndpoint` 必須符合 node identity，且不得與既有 node 衝突。
- public key 必須唯一；同一 node 對同一 generation 的 retry 除外。
- re-registration、key rotation、reject、revoke 和 expire 必須能在 audit/status output 中看到。
- rate limiting 保護 bootstrap endpoint，避免無效 registration 連續衝擊。

`routerctl` 應將 enrollment state 顯示為 `Pending`、`Approved`、`Rejected`、`Revoked`
或 `Expired`，並在 request 未啟用時顯示驗證原因。

## 非目標

- 不替換 WireGuard cryptokey routing。RR/spine 仍為每個已核准 leaf 安裝一個 kernel peer。
- 不在沒有顯式 policy decision 的情況下接受任意 public key。
- 不透過目標 WireGuard interface 執行首次 enrollment。
- 不讓 `SAMNodeSet` 分發依賴一個本身需要新 peer 才能建立的 tunnel。

## 實施計畫

1. 定義 enrollment API resource 形狀、status model 和 CLI/status output，並與 WireGuard runtime reconcile 分離。
2. 將 RR/spine enrollment storage 作為 dynamic config source 增加，並保留持久 audit 資訊和 stale entry cleanup。
3. 增加基於 policy 和可選 `SAMNodeSet` membership 的 validation。
4. 在保留 static peer override 行為的同時，把已核准 registration 輸入既有效 effective WireGuard peer generation path。
5. 增加可在 boot 時安全執行的、冪等的 leaf-side submit/retry logic。
6. 增加 revoke 和 key rotation flow。

## 影響

如果部署具有 approved enrollment policy，RR/spine config 就不再需要隨著每個 leaf 增加而手寫一個
WireGuard peer block。kernel peer 數和 identity validation 仍然存在，但 operator workflow
會從逐個編輯 RR/spine peer material 轉向核准或預授權 leaf registration。

該功能也明確了 bootstrap boundary：topology distribution 繼續使用 `SAMNodeSet` 和 `peersFrom`，
first-contact trust 則由顯式的 non-WireGuard enrollment surface 處理。

The same boundary extends into RR-side BGP admission. Generated
route-reflector client peers derive import filters from the SAM topology and
configured mobility prefix allowlist: each leaf may import only `/32` routes
under the permitted prefixes, must attach its own node-identity community, and
is rejected if it attaches another topology node's identity. The holder-beacon
community is valid only together with the leaf's own identity and an allowed host
route; it is not a standalone authority.
