# 套用與復原

設定是套用內容的依據。調節會修復缺少或變更的設定物件，並移除設定及有效動態狀態不需要的物件。手動建立的物件不會僅因不是routerd建立而受到保護。OS基礎及運作所需的DHCP、PD、BGP、SAM動態狀態不屬於靜態設定。

--replace須搭配完整設定。不使用--replace時，部分更新不會刪除所有省略的資源。請檢查刪除計畫並保留獨立主控台。

```sh
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl get status
routerctl get events --limit 20
```

常駐程序負責執行中的狀態調節。validate或plan成功不代表流量正常。套用後須檢查狀態、事件及實際對端連線。

```sh
routerd serve --config router.yaml
```

復原是重新套用已儲存的設定，不會復原過去的核心狀態、連線或租約。

```sh
routerctl rollback --list
routerctl rollback --to <generation>
```

import是明確匯入選定實體介面及靜態位址的操作。結果須先整合進完整設定，再使用--replace；不要將取得的動態狀態匯入靜態YAML。

```sh
routerctl import --interface ens18 --address 192.0.2.1/24 --config router.yaml > candidate.yaml
```

[DB](state-database.md) · [YAML](../reference/resources.md)
