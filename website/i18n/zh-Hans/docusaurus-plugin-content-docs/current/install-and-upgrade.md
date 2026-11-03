---
title: 安装与升级
---

# 安装与升级

routerd 1.5 提案版面向全新安装的 OS，使用 Debian/Ubuntu `.deb` 或 FreeBSD `.pkg`。
此功能仍在开发中，旧 stable release 的归档不是 1.5 软件包，目前也未公布公共
apt/pkg 仓库。先获取与目标版本、OS 和 CPU 匹配的软件包，并核对校验和。

```sh
# Debian/Ubuntu
sudo apt install ./routerd_<version>_amd64.deb
# FreeBSD 14
sudo pkg -o HANDLE_RC_SCRIPTS=true install ./routerd-<version>.pkg
```

请替换为实际文件名；arm64 主机使用 arm64 软件包。
首次安装只放置文件和服务定义，不启动 daemon，也不应用示例配置。
Linux 配置路径为 `/etc/routerd/router.yaml`，状态为 `/var/lib/routerd`；
FreeBSD 配置为 `/usr/local/etc/routerd/router.yaml`，状态为 `/var/db/routerd`。
示例位于 Linux `/usr/share/doc/routerd/examples` 或
FreeBSD `/usr/local/share/doc/routerd/examples`，不能直接作为所有环境的安全配置。

审核配置并保留独立管理路径后，先执行 `routerd validate --config <实际路径>`。
Linux 使用 `sudo systemctl enable --now routerd.service`；FreeBSD 使用
`sudo sysrc routerd_enable=YES` 和 `sudo service routerd start`。
随后从独立客户端检查真实通信；安装成功不代表路由功能已经验证。

更新使用同样的 apt/pkg 命令安装新版软件包，保留配置和持久状态。
Debian 更新会停止旧的自有 daemon/helper，替换文件，再恢复此前运行的服务。
FreeBSD 更新须使用上面的 `HANDLE_RC_SCRIPTS=true`，并为运行中的路由器设置
`routerd_enable=YES`；pkg 在 hook 清理之后启动已启用的 rc.d 服务。因此，FreeBSD
服务即使更新前已停止，只要启用也可能启动。此选项仅作用于当前 pkg 命令，不修改全局设置。
不使用此服务策略可能导致更新后服务保持停止。更新可能中断通信。
无法确认停止成功时拒绝替换。不要用手动复制覆盖软件包拥有的文件。
不提供 1.0、`make install`、手动复制或归档安装的迁移与接管路径。

删除使用 `sudo apt remove routerd` 或 `sudo pkg delete routerd`。
删除后保留配置和数据库，包括 Debian purge。撤除路由器前，应先撤回配置资源并
核验实际网络状态；卸载本身不保证撤回全部网络效果。

完整路径、更新边界和可重复的测试方法见[英文导入指南](/docs/install-and-upgrade)。
发布 workflow 的定义不等于已发布，也不等于所有 CPU 和全部路由功能已通过验证。
