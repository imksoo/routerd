# MobilityPool

[各資源的設定](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

結合共用 SAM 拓撲、本機所有權觀察及 BGP/聯邦狀態，規劃位址移動與本機擷取。YAML 是本機意圖，共用身分位於 SAMNodeSet。

## 指定條件與參照關係

- prefix 須為 IPv4，groupRef 須解析至有 nodeName 的本機 EventGroup，該節點須存在於必要 membersFrom 拓撲。各成員需非空 site 及 onprem/cloud 角色，僅本機可帶擷取/探索/靜態所有權覆寫。
- onprem proxy-ARP 需 capture.interface 及明確 single-router/vrrp-master activeWhen。供應者擷取需 providerRef 及 nicRef（探索 NIC 時例外），route-table 另需 target.routeTableRef；供應者擷取禁用 activeWhen。
- Linux 使用每介面 proxy-ARP 控制，FreeBSD 使用依路由判斷的全域 proxyall，而非 Linux sysctl 名稱。擷取啟用仍依實際所有權/就緒事實，有效 YAML 或計畫意圖不證明延伸網段可運作。
- 設定以非零語意合併，空字串/清單及 false 不清除繼承欄位。來源位址固定值優先狀態來源；map 以本機值優先合併，target 固定值再優先於 targetFrom。設定引用僅限 cloud。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    [ members:
        -
          [ capture:
              [ activeWhen:
                  [ type: ("" | "single-router" | "vrrp-master") ]
                  [ virtualAddressRef: <string> ]
              ]
              [ captureStrategy: ("" | "route-table") ]
              [ excludeAddresses:
                  - <string>
              ]
              [ gratuitousARP: <boolean> ]
              [ interface: <string> ]
              [ nicRef: <string> ]
              [ providerRef: <string> ]
              [ sourceAddress: <string> ]
              [ sourceAddressFrom:
                  field: <string>
                  [ optional: <boolean> ]
                  resource: <string>
              ]
              [ target:
                  [ <name>: <string> ]
              ]
              [ targetFrom:
                  [ <name>: <string> ]
              ]
              [ type: ("provider-secondary-ip" | "proxy-arp") ]
          ]
          nodeRef: <string>
          [ ownershipDiscovery:
              [ allowEmptyAfter: <string> ]
              [ leaseTTL: <string> ]
              [ mode: ("" | "disabled" | "provider-private-ip" | "onprem-l2") ]
              [ pluginRef: <string> ]
              [ providerRef: <string> ]
              [ scanInterval: <string> ]
              [ scope:
                  [ excludeAddresses:
                      - <string>
                  ]
                  [ includeAddresses:
                      - <string>
                  ]
              ]
              [ selector:
                  [ tags:
                      [ <name>: <string> ]
                  ]
              ]
              [ sources:
                  -
                    [ bridge: <string> ]
                    [ interface: <string> ]
                    [ leaseTTL: <string> ]
                    [ network: <string> ]
                    [ probeRetries: <integer> ]
                    [ probeTimeout: <string> ]
                    [ resource: <string> ]
                    [ scanInterval: <string> ]
                    [ sourceAddressFrom:
                        field: <string>
                        [ optional: <boolean> ]
                        resource: <string>
                    ]
                    type: ("dhcpv4-lease" | "arp-observer" | "on-demand-arp" | "pve-svnet")
              ]
              [ stoppedInstancePolicy: ("" | "hold" | "release") ]
              [ subnetRef: <string> ]
              [ subnetRefFrom: <string> ]
          ]
          [ profileRef: <string> ]
          [ staticOwnedAddresses:
              - <string>
          ]
    ]
    membersFrom:
      -
        [ optional: <boolean> ]
        resource: <string>
    prefix: <string>
    [ profiles:
        [ cloudCaptures:
            [ <name>:
                [ capture:
                    [ activeWhen:
                        [ type: ("" | "single-router" | "vrrp-master") ]
                        [ virtualAddressRef: <string> ]
                    ]
                    [ captureStrategy: ("" | "route-table") ]
                    [ excludeAddresses:
                        - <string>
                    ]
                    [ gratuitousARP: <boolean> ]
                    [ interface: <string> ]
                    [ nicRef: <string> ]
                    [ providerRef: <string> ]
                    [ sourceAddress: <string> ]
                    [ sourceAddressFrom:
                        field: <string>
                        [ optional: <boolean> ]
                        resource: <string>
                    ]
                    [ target:
                        [ <name>: <string> ]
                    ]
                    [ targetFrom:
                        [ <name>: <string> ]
                    ]
                    [ type: ("provider-secondary-ip" | "proxy-arp") ]
                ]
                [ ownershipDiscovery:
                    [ allowEmptyAfter: <string> ]
                    [ leaseTTL: <string> ]
                    [ mode: ("" | "disabled" | "provider-private-ip" | "onprem-l2") ]
                    [ pluginRef: <string> ]
                    [ providerRef: <string> ]
                    [ scanInterval: <string> ]
                    [ scope:
                        [ excludeAddresses:
                            - <string>
                        ]
                        [ includeAddresses:
                            - <string>
                        ]
                    ]
                    [ selector:
                        [ tags:
                            [ <name>: <string> ]
                        ]
                    ]
                    [ sources:
                        -
                          [ bridge: <string> ]
                          [ interface: <string> ]
                          [ leaseTTL: <string> ]
                          [ network: <string> ]
                          [ probeRetries: <integer> ]
                          [ probeTimeout: <string> ]
                          [ resource: <string> ]
                          [ scanInterval: <string> ]
                          [ sourceAddressFrom:
                              field: <string>
                              [ optional: <boolean> ]
                              resource: <string>
                          ]
                          type: ("dhcpv4-lease" | "arp-observer" | "on-demand-arp" | "pve-svnet")
                    ]
                    [ stoppedInstancePolicy: ("" | "hold" | "release") ]
                    [ subnetRef: <string> ]
                    [ subnetRefFrom: <string> ]
                ]
            ]
        ]
    ]
    [ staticHandovers:
        -
          address: <string>
          fromNodeRef: <string>
          toNodeRef: <string>
    ]
    [ values:
        [ <name>: <string> ]
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.prefix` | **必填** | string | 必填集區管理的 IPv4 CIDR，所有權/擷取位址須在其內，無預設前綴或自動位址分配。 |
| `spec.groupRef` | **必填** | string | 必填具有 nodeName 的本機 EventGroup 參照，其本機節點識別須存在於 membersFrom 拓撲，用於所有權及到期事件。 |
| `spec.values` | *選填* | object | 選用供應者識別字串常數，供 targetFrom/subnetRefFrom 參照，不自動展開變數或儲存密鑰。 |
| `spec.profiles` | *選填* | object | 選用可重用本機擷取/探索預設值，不包含共用拓撲身分。 |
| `spec.profiles.cloudCaptures` | *選填* | object | 以設定名稱為鍵的 map，僅供本機 cloud 成員 profileRef 選用，不支援 onprem 簡寫。包含未選用設定在內，所有 target/value 參照皆會正規化。 |
| `spec.membersFrom` | **必填** | array; minItems=1 | 必填且非空 SAMNodeSet 來源清單，是成員身分、site、role、placement、maintenance 及容量唯一來源，不由 members 覆寫補入拓撲。 |
| `spec.membersFrom[].resource` | **必填** | string | 必填 SAMNodeSet/name。匯入 nodeRef 不可重複，每個解析成員需明確 site 及 onprem/cloud 角色。 |
| `spec.membersFrom[].optional` | *選填* | boolean | 預設 false。非 optional 執行時來源缺少為 Pending，true 可容忍，但不能免除 EventGroup 本機節點必須在拓撲解析的要求。 |
| `spec.members` | *選填* | array | 選用本機專屬覆寫，nodeRef 須已由 membersFrom 匯入。不可設定共用 site/role/placement/maintenance/容量，也不可設定遠端擷取。 |
| `spec.members[].nodeRef` | **條件必填** | string | 必填已匯入 nodeRef，選取 EventGroup 本機成員，不能以覆寫新增成員。 |
| `spec.members[].profileRef` | *選填* | string | 選用 profiles.cloudCaptures 鍵，僅支援 cloud，缺少設定使正規化失敗。非空字串/清單覆寫，map 合併，true 布林為累加，空清單/false 無法清除繼承值。 |
| `spec.members[].staticOwnedAddresses` | *選填* | array | 選用 prefix 內明確 IPv4 /32 CIDR，僅 onprem。此處拒絕純 IPv4，靜態所有權不可重複，這些宣告不依賴學習用戶端事件。 |
| `spec.staticHandovers` | *選填* | array | 選用計畫靜態所有權移轉，觀察到來源成員釋放後才投影目標，不是同時雙重擁有。 |
| `spec.staticHandovers[].address` | **條件必填** | string | 必填集區內 IPv4 /32 CIDR，移轉間須唯一。有靜態擁有宣告時，擁有者須等於 fromNodeRef。 |
| `spec.staticHandovers[].fromNodeRef` | **條件必填** | string | 必填既有已匯入 onprem 成員 nodeRef，須不同於 toNodeRef。 |
| `spec.staticHandovers[].toNodeRef` | **條件必填** | string | 必填既有已匯入目標 nodeRef，須不同於來源，不隱含建立目標成員。 |
| `spec.members[].capture` | *選填* | object | 本機擷取設定，type 須明確指定，不從 role 推定。僅本機成員可帶擷取/探索覆寫。 |
| `spec.members[].capture.type` | *選填* | string; enum=["provider-secondary-ip", "proxy-arp"] | onprem 使用 proxy-arp，cloud 使用 provider-secondary-ip。空值表示無擷取機制，但本機 cloud 成員需解析的擷取設定。 |
| `spec.members[].capture.providerRef` | *選填* | string | provider-secondary-ip 必填的 CloudProviderProfile 名稱，不是狀態來源，亦為所有權探索 providerRef 預設值。 |
| `spec.members[].capture.captureStrategy` | *選填* | string; enum=["", "route-table"] | 僅 provider-secondary-ip 可選用 route-table，需 target.routeTableRef。一般 secondary-IP/proxy-ARP 請省略，明確 secondary-ip/proxy-arp 策略值會被拒絕。 |
| `spec.members[].capture.nicRef` | *選填* | string | 供應者 NIC 識別，除可由 provider-private-ip 探索解析外，供應者擷取必填。不可放在 target/targetFrom 下，該寫法會被拒絕。 |
| `spec.members[].capture.interface` | *選填* | string | proxy-arp 必填的邏輯/OS 擷取介面，由使用端解析別名，不自動選擇 LAN。 |
| `spec.members[].capture.sourceAddress` | *選填* | string | proxy-ARP/BGP 傳送使用的選用集區內本機 IPv4 來源。固定值優先 sourceAddressFrom，此處不檢查互斥；有效時衍生 /32 位址及偏好來源，不是遠端擁有者位址。 |
| `spec.members[].capture.sourceAddressFrom` | *選填* | object | 選用來源位址狀態參照，接受有效集區內 IPv4，缺少或無效值仍不可用。不同於固定值，不產生 IPv4StaticAddress，因來源資源管理位址生命週期。 |
| `spec.members[].capture.excludeAddresses` | *選填* | array | 選用排除於 proxy-ARP 擷取的集區內 IPv4 位址/CIDR，預設無。供僅本機閘道等使用，不是所有 BGP 路由篩選。 |
| `spec.members[].capture.gratuitousARP` | *選填* | boolean | 預設 false。true 要求擷取變更時發送非請求 ARP 通告。設定繼承為累加，本機 false 不清除 profile 的 true。 |
| `spec.members[].capture.activeWhen` | *選填* | object | onprem proxy-arp 必填的明確權限條件，provider-secondary-ip 禁用，不是一般 state/all/any 條件。 |
| `spec.members[].capture.activeWhen.type` | *選填* | string; enum=["", "single-router", "vrrp-master"] | single-router 無條件授權本機 proxy-ARP；vrrp-master 需參照 VirtualAddress 的 MASTER 狀態。proxy-ARP 無預設值。 |
| `spec.members[].capture.activeWhen.virtualAddressRef` | *選填* | string | vrrp-master 必填既有本機 VirtualAddress 名稱（也接受 VirtualAddress/name），single-router 禁用，僅對本機成員檢查。 |
| `spec.members[].capture.target` | *選填* | object | 選用非秘密供應者目標 map。鍵值不可為空，拒絕 secret/token/password/credential/private-key/access-key 類鍵。明確值優先於 targetFrom。 |
| `spec.members[].capture.targetFrom` | *選填* | object | 選用從供應者目標鍵對應 spec.values 鍵的 map，參照常數缺少或空白會使正規化失敗。禁用 nicRef，請用 capture.nicRef。 |
| `spec.members[].capture.sourceAddressFrom.resource` | **條件必填** | string | 來源 Kind/name，共用驗證要求資源存在。 |
| `spec.members[].capture.sourceAddressFrom.field` | **條件必填** | string | resource 設定時必填來源提供欄位。 |
| `spec.members[].capture.sourceAddressFrom.optional` | *選填* | boolean | 預設 false，不免除參照驗證或補入缺少來源。 |
| `spec.members[].ownershipDiscovery` | *選填* | object | 本機所有權事實探索設定，預設停用，僅本機成員可設定；接收的遠端事件為另一輸入。 |
| `spec.members[].ownershipDiscovery.mode` | *選填* | string; enum=["", "disabled", "provider-private-ip", "onprem-l2"] | 空/disabled 不執行。provider-private-ip 需 cloud/provider-secondary-ip；onprem-l2 需 onprem/proxy-arp、capture.interface 及非空 sources。 |
| `spec.members[].ownershipDiscovery.providerRef` | *選填* | string | 供應者盤點設定名稱，預設 capture.providerRef，由 provider-private-ip 使用，不是事件資源參照。 |
| `spec.members[].ownershipDiscovery.pluginRef` | *選填* | string | 選用盤點 Plugin 名稱，空值依供應者名稱或唯一具 observe.providerPrivateIPs 能力的外掛解析；歧義或能力缺少使探索失敗。 |
| `spec.members[].ownershipDiscovery.subnetRef` | *選填* | string | 選用供應者子網識別，明確值優先於 subnetRefFrom，空值可由支援的外掛從自身 NIC 推定。 |
| `spec.members[].ownershipDiscovery.subnetRefFrom` | *選填* | string | 僅 subnetRef 為空時使用的選用 spec.values 鍵。常數缺少或為空使正規化失敗，不是 Kind/name/field。 |
| `spec.members[].ownershipDiscovery.scanInterval` | *選填* | string | 供應者掃描間隔，預設 60 秒，明確值 >=30 秒。onprem-l2 也會驗證，但事件來源不需供應者掃描，觀察輪詢使用各來源 scanInterval。 |
| `spec.members[].ownershipDiscovery.leaseTTL` | *選填* | string | 所有權事件的正 Go duration，預設 5 分鐘，onprem 觀察由各來源 leaseTTL 覆寫。是所有權租約，不是重新啟動期限。 |
| `spec.members[].ownershipDiscovery.stoppedInstancePolicy` | *選填* | string; enum=["", "hold", "release"] | 僅 provider-private-ip：hold/省略保留停止執行個體的所有權事實，release 使其過期。其他模式禁止非空值。 |
| `spec.members[].ownershipDiscovery.allowEmptyAfter` | *選填* | string | 僅 onprem-l2，來源啟動後正數等待期間，之後零用戶端可視為完成；省略則零用戶端被動探索仍待處理。使用 profileRef 時目前合併不複製本機此欄位覆寫，保留 profile 值。 |
| `spec.members[].ownershipDiscovery.sources` | *選填* | array | onprem-l2 必填非空清單，provider-private-ip 禁用。拒絕重複的 type/resource/有效介面/network/bridge 組合。 |
| `spec.members[].ownershipDiscovery.sources[].type` | **條件必填** | string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"] | 必填 dhcpv4-lease/arp-observer/on-demand-arp/pve-svnet。ARP/PVE 來源衍生本機觀察精靈意圖，DHCP 事實來自實際租約事件。 |
| `spec.members[].ownershipDiscovery.sources[].resource` | *選填* | string | 選用來源識別字串，預設為空，用於去重及觀察身分指紋，不是觀察程序名稱或驗證存在的 Kind/name 參照。事件主要依 type/interface/network/bridge 比對。 |
| `spec.members[].ownershipDiscovery.sources[].interface` | *選填* | string | 選用觀察介面，預設 capture.interface。on-demand-arp 在替代解析後必填，使用端將宣告別名解析為 OS 名稱。 |
| `spec.members[].ownershipDiscovery.sources[].bridge` | *選填* | string | 選用本機橋接標籤，供 PVE/svnet 觀察比對。預設空值不限制 bridge，不要求建立 Bridge。 |
| `spec.members[].ownershipDiscovery.sources[].network` | *選填* | string | 選用 PVE/svnet 邏輯網路標籤，例如 svnet1。空值不限制 network 選擇，不是 IP CIDR。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom` | *選填* | object | 選用主動 ARP 傳送來源狀態。缺少或無法使用時改用已驗證擷取來源位址；兩者皆無法解析時不產生 on-demand probe 意圖。 |
| `spec.members[].ownershipDiscovery.sources[].scanInterval` | *選填* | string | 選用觀察輪詢間隔，明確值 >=1 秒；產生 ARP/PVE 精靈預設 10 秒，事件驅動 DHCP 忽略輪詢。 |
| `spec.members[].ownershipDiscovery.sources[].probeTimeout` | *選填* | string | 主動 ARP 重試之間的正 Go duration，觀察精靈預設 1 秒，僅供 on-demand probing。 |
| `spec.members[].ownershipDiscovery.sources[].probeRetries` | *選填* | integer; minimum=0; maximum=20 | 整數 0–20 額外 ARP 重試，預設 0，不是總嘗試次數，初次嘗試另計。 |
| `spec.members[].ownershipDiscovery.sources[].leaseTTL` | *選填* | string | 選用正數事件租約期間，覆寫此來源 ownershipDiscovery.leaseTTL，省略繼承集區探索租約（預設 5 分鐘）。 |
| `spec.members[].ownershipDiscovery.scope` | *選填* | object | 選用集區內所有權觀察位址篩選，不授權任意外部前綴廣告。 |
| `spec.members[].ownershipDiscovery.scope.includeAddresses` | *選填* | array | 選用集區內 IPv4 純位址/CIDR 允許清單，空值允許所有候選集區位址。有效分片指派可取代此供應者探索清單。 |
| `spec.members[].ownershipDiscovery.scope.excludeAddresses` | *選填* | array | 選用集區內 IPv4 純位址/CIDR 排除清單，預設無，在包含比對後排除。 |
| `spec.members[].ownershipDiscovery.selector` | *選填* | object | 選用供應者盤點標籤選擇器，不變更雲端標籤。 |
| `spec.members[].ownershipDiscovery.selector.tags` | *選填* | object | 非空標籤鍵與預期字串值，所有鍵的修剪值皆須符合，空 map 不限制。供供應者盤點使用，不用於 onprem 事件來源比對。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.resource` | **條件必填** | string | 使用狀態來源時必填既有 Kind/name。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.field` | **條件必填** | string | resource 設定時必填來源提供欄位。 |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.optional` | *選填* | boolean | 預設 false，不免除參照驗證，也不補入未解析傳送來源。 |
| `spec.profiles.cloudCaptures.<name>.capture` | *選填* | object | 本機擷取設定，type 須明確指定，不從 role 推定。僅本機成員可帶擷取/探索覆寫。 |
| `spec.profiles.cloudCaptures.<name>.capture.type` | *選填* | string; enum=["provider-secondary-ip", "proxy-arp"] | onprem 使用 proxy-arp，cloud 使用 provider-secondary-ip。空值表示無擷取機制，但本機 cloud 成員需解析的擷取設定。 |
| `spec.profiles.cloudCaptures.<name>.capture.providerRef` | *選填* | string | provider-secondary-ip 必填的 CloudProviderProfile 名稱，不是狀態來源，亦為所有權探索 providerRef 預設值。 |
| `spec.profiles.cloudCaptures.<name>.capture.captureStrategy` | *選填* | string; enum=["", "route-table"] | 僅 provider-secondary-ip 可選用 route-table，需 target.routeTableRef。一般 secondary-IP/proxy-ARP 請省略，明確 secondary-ip/proxy-arp 策略值會被拒絕。 |
| `spec.profiles.cloudCaptures.<name>.capture.nicRef` | *選填* | string | 供應者 NIC 識別，除可由 provider-private-ip 探索解析外，供應者擷取必填。不可放在 target/targetFrom 下，該寫法會被拒絕。 |
| `spec.profiles.cloudCaptures.<name>.capture.interface` | *選填* | string | proxy-arp 必填的邏輯/OS 擷取介面，由使用端解析別名，不自動選擇 LAN。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddress` | *選填* | string | proxy-ARP/BGP 傳送使用的選用集區內本機 IPv4 來源。固定值優先 sourceAddressFrom，此處不檢查互斥；有效時衍生 /32 位址及偏好來源，不是遠端擁有者位址。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom` | *選填* | object | 選用來源位址狀態參照，接受有效集區內 IPv4，缺少或無效值仍不可用。不同於固定值，不產生 IPv4StaticAddress，因來源資源管理位址生命週期。 |
| `spec.profiles.cloudCaptures.<name>.capture.excludeAddresses` | *選填* | array | 選用排除於 proxy-ARP 擷取的集區內 IPv4 位址/CIDR，預設無。供僅本機閘道等使用，不是所有 BGP 路由篩選。 |
| `spec.profiles.cloudCaptures.<name>.capture.gratuitousARP` | *選填* | boolean | 預設 false。true 要求擷取變更時發送非請求 ARP 通告。設定繼承為累加，本機 false 不清除 profile 的 true。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen` | *選填* | object | onprem proxy-arp 必填的明確權限條件，provider-secondary-ip 禁用，不是一般 state/all/any 條件。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.type` | *選填* | string; enum=["", "single-router", "vrrp-master"] | single-router 無條件授權本機 proxy-ARP；vrrp-master 需參照 VirtualAddress 的 MASTER 狀態。proxy-ARP 無預設值。 |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.virtualAddressRef` | *選填* | string | vrrp-master 必填既有本機 VirtualAddress 名稱（也接受 VirtualAddress/name），single-router 禁用，僅對本機成員檢查。 |
| `spec.profiles.cloudCaptures.<name>.capture.target` | *選填* | object | 選用非秘密供應者目標 map。鍵值不可為空，拒絕 secret/token/password/credential/private-key/access-key 類鍵。明確值優先於 targetFrom。 |
| `spec.profiles.cloudCaptures.<name>.capture.targetFrom` | *選填* | object | 選用從供應者目標鍵對應 spec.values 鍵的 map，參照常數缺少或空白會使正規化失敗。禁用 nicRef，請用 capture.nicRef。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.resource` | **條件必填** | string | 來源 Kind/name，共用驗證要求資源存在。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.field` | **條件必填** | string | resource 設定時必填來源提供欄位。 |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.optional` | *選填* | boolean | 預設 false，不免除參照驗證或補入缺少來源。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery` | *選填* | object | 本機所有權事實探索設定，預設停用，僅本機成員可設定；接收的遠端事件為另一輸入。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.mode` | *選填* | string; enum=["", "disabled", "provider-private-ip", "onprem-l2"] | 空/disabled 不執行。provider-private-ip 需 cloud/provider-secondary-ip；onprem-l2 需 onprem/proxy-arp、capture.interface 及非空 sources。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.providerRef` | *選填* | string | 供應者盤點設定名稱，預設 capture.providerRef，由 provider-private-ip 使用，不是事件資源參照。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.pluginRef` | *選填* | string | 選用盤點 Plugin 名稱，空值依供應者名稱或唯一具 observe.providerPrivateIPs 能力的外掛解析；歧義或能力缺少使探索失敗。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRef` | *選填* | string | 選用供應者子網識別，明確值優先於 subnetRefFrom，空值可由支援的外掛從自身 NIC 推定。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRefFrom` | *選填* | string | 僅 subnetRef 為空時使用的選用 spec.values 鍵。常數缺少或為空使正規化失敗，不是 Kind/name/field。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scanInterval` | *選填* | string | 供應者掃描間隔，預設 60 秒，明確值 >=30 秒。onprem-l2 也會驗證，但事件來源不需供應者掃描，觀察輪詢使用各來源 scanInterval。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.leaseTTL` | *選填* | string | 所有權事件的正 Go duration，預設 5 分鐘，onprem 觀察由各來源 leaseTTL 覆寫。是所有權租約，不是重新啟動期限。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.stoppedInstancePolicy` | *選填* | string; enum=["", "hold", "release"] | 僅 provider-private-ip：hold/省略保留停止執行個體的所有權事實，release 使其過期。其他模式禁止非空值。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.allowEmptyAfter` | *選填* | string | 僅 onprem-l2，來源啟動後正數等待期間，之後零用戶端可視為完成；省略則零用戶端被動探索仍待處理。使用 profileRef 時目前合併不複製本機此欄位覆寫，保留 profile 值。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources` | *選填* | array | onprem-l2 必填非空清單，provider-private-ip 禁用。拒絕重複的 type/resource/有效介面/network/bridge 組合。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].type` | **條件必填** | string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"] | 必填 dhcpv4-lease/arp-observer/on-demand-arp/pve-svnet。ARP/PVE 來源衍生本機觀察精靈意圖，DHCP 事實來自實際租約事件。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].resource` | *選填* | string | 選用來源識別字串，預設為空，用於去重及觀察身分指紋，不是觀察程序名稱或驗證存在的 Kind/name 參照。事件主要依 type/interface/network/bridge 比對。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].interface` | *選填* | string | 選用觀察介面，預設 capture.interface。on-demand-arp 在替代解析後必填，使用端將宣告別名解析為 OS 名稱。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].bridge` | *選填* | string | 選用本機橋接標籤，供 PVE/svnet 觀察比對。預設空值不限制 bridge，不要求建立 Bridge。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].network` | *選填* | string | 選用 PVE/svnet 邏輯網路標籤，例如 svnet1。空值不限制 network 選擇，不是 IP CIDR。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom` | *選填* | object | 選用主動 ARP 傳送來源狀態。缺少或無法使用時改用已驗證擷取來源位址；兩者皆無法解析時不產生 on-demand probe 意圖。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].scanInterval` | *選填* | string | 選用觀察輪詢間隔，明確值 >=1 秒；產生 ARP/PVE 精靈預設 10 秒，事件驅動 DHCP 忽略輪詢。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeTimeout` | *選填* | string | 主動 ARP 重試之間的正 Go duration，觀察精靈預設 1 秒，僅供 on-demand probing。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeRetries` | *選填* | integer; minimum=0; maximum=20 | 整數 0–20 額外 ARP 重試，預設 0，不是總嘗試次數，初次嘗試另計。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].leaseTTL` | *選填* | string | 選用正數事件租約期間，覆寫此來源 ownershipDiscovery.leaseTTL，省略繼承集區探索租約（預設 5 分鐘）。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope` | *選填* | object | 選用集區內所有權觀察位址篩選，不授權任意外部前綴廣告。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.includeAddresses` | *選填* | array | 選用集區內 IPv4 純位址/CIDR 允許清單，空值允許所有候選集區位址。有效分片指派可取代此供應者探索清單。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.excludeAddresses` | *選填* | array | 選用集區內 IPv4 純位址/CIDR 排除清單，預設無，在包含比對後排除。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector` | *選填* | object | 選用供應者盤點標籤選擇器，不變更雲端標籤。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector.tags` | *選填* | object | 非空標籤鍵與預期字串值，所有鍵的修剪值皆須符合，空 map 不限制。供供應者盤點使用，不用於 onprem 事件來源比對。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.resource` | **條件必填** | string | 使用狀態來源時必填既有 Kind/name。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.field` | **條件必填** | string | resource 設定時必填來源提供欄位。 |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.optional` | *選填* | boolean | 預設 false，不免除參照驗證，也不補入未解析傳送來源。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller.go) · [pkg/controller/mobility/controller_effects.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller_effects.go) · [pkg/controller/mobility/bgp_delivery_planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/bgp_delivery_planner.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go) · [pkg/controller/mobility/discovery_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_support.go) · [pkg/controller/mobility/discovery_facts.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_facts.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/pool_plan.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/pool_plan.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/chain/sam.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam.go) · [pkg/controller/chain/sam_freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam_freebsd.go) · [cmd/routerd-arp-observer/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-arp-observer/main.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
