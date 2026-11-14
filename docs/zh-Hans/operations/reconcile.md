# 应用与恢复

配置是应用内容的依据。协调过程会修复缺失或变更的配置对象，并移除配置及有效动态状态不需要的对象。手动创建的对象不会仅因不是routerd创建而受到保护。OS基础及运行所需的DHCP、PD、BGP、SAM动态状态不属于静态配置。

--replace须搭配完整配置。不使用--replace时，部分更新不会删除所有省略的资源。请检查删除计划并保留独立控制台。

```sh
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl get status
routerctl get events --limit 20
```

常驻进程负责运行中的状态协调。validate或plan成功不代表流量正常。应用后须检查状态、事件及实际对端连接。

```sh
routerd serve --config router.yaml
```

恢复是重新应用已保存的配置，不会恢复过去的内核状态、连接或租约。

```sh
routerctl rollback --list
routerctl rollback --to <generation>
```

import是显式导入所选物理接口及静态地址的操作。结果须先整合进完整配置，再使用--replace；不要将获取的动态状态导入静态YAML。

```sh
routerctl import --interface ens18 --address 192.0.2.1/24 --config router.yaml > candidate.yaml
```

[DB](state-database.md) · [YAML](../reference/resources.md)
