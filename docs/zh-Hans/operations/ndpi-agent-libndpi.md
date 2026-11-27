---
title: nDPI agent native package
---

# nDPI 代理程序原生软件包

routerd 的 Linux 二进制文件以 `CGO_ENABLED=0` 构建并静态链接。可选的 `routerd-ndpi` Debian 软件包是为需要原生 nDPI 分类的主机提供的例外。其中只包含 `/usr/local/sbin/routerd-ndpi-agent`，以 `CGO_ENABLED=1 -tags libndpi` 构建并链接系统的 `libndpi`。`routerd` 软件包不包含 nDPI 代理程序。

## 安装

与 `routerd` 软件包一起安装。对应的 `libndpi` 运行时由 apt 根据软件包依赖安装。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb.sha256
sha256sum -c routerd-ndpi-linux-amd64.deb.sha256
sudo apt install ./routerd-ndpi-linux-amd64.deb
```

更新软件包时，若 `routerd-ndpi-agent.service` 已启用或正在运行，则会重新启动。确认原生后端是否生效：

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

响应中应包含 `"libndpiLoaded": true`。

## 配置

启用 `FirewallEventLog` 资源。routerd 随后启动
`routerd-firewall-logger` 和 `routerd-ndpi-agent`，日志程序通过 Unix
socket 将采样数据包直接发送给代理程序。不再使用独立的分类进程，也没有内置分类器的后备路径。
