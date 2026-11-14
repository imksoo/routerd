# 安装与更新

下载官方Linux amd64归档并验证校验和。其他OS或架构请在Releases选择对应文件。安装程序保留现有配置。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

[Releases](https://github.com/imksoo/routerd/releases/latest)

使用前修改接口名、地址与对端配置。先在本地验证，再在目标路由器检查计划。--replace只用于完整配置。应用可能影响管理连接，请保留控制台。

```sh
routerd validate --config router.yaml
# On an existing router:
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
```

现有路由器应先检查差异，再通过routerctl应用。新的Linux主机则放置确认后的配置并启动routerd.service。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo routerctl get status
sudo routerctl get events --limit 20
```

Live ISO的工作根目录与状态DB是临时空间。YAML与秘密信息须放在已配置的持久介质。不要更换运行中VM的ISO；先关机并确认停止，再更换介质并启动。

[Live ISO](https://github.com/imksoo/routerd/blob/main/docs/how-to/pve-live-iso-nocloud.md) · [USB](operations/usb-persistence.md) · [DB](operations/state-database.md)
