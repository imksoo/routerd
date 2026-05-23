---
title: nDPI agent native package
---

# nDPI 代理程序原生软件包

routerd 的一般 Linux 发布归档以 `CGO_ENABLED=0` 构建，确保归档内所有 routerd 二进制文件均为静态链接。可选的 `routerd-ndpi-agent-libndpi` 归档是针对需要原生 nDPI 分类功能的主机所提供的例外软件包。

此归档仅包含下列项目：

- `bin/routerd-ndpi-agent`
- `share/doc/README.md`
- `share/doc/VERSION`
- `share/doc/TARGET`

此二进制文件以 `CGO_ENABLED=1 -tags libndpi` 构建，并链接至目标系统的 `libndpi` 运行期库。它的用途是覆盖已安装一般 routerd 归档之主机上的对应二进制文件。

## Install

同时下载一般 routerd 发布归档与对应的原生代理程序归档，并让安装程序以单一事务方式应用。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

主机需要安装与归档构建时具有相同共享库 ABI 的 `libndpi` 运行期软件包。在 Debian/Ubuntu 上，可通过以下命令安装可选的运行期依赖：

```sh
sudo apt-get install libndpi-bin
```

确认原生后端是否已启用：

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

响应中应包含 `"libndpiLoaded": true`。

## Upgrade note

一般 routerd 归档内含默认的静态版 `routerd-ndpi-agent`。
升级时，若现有原生代理程序的 `selftest` 返回 `"libndpiLoaded": true`，而归档中的代理程序不返回，则 `install.sh` 会保留现有的原生代理程序。

若主机需要原生应用层分类功能，请在执行一般安装程序时加上 `--with-ndpi`。若最终安装的代理程序未返回 `"libndpiLoaded": true`，安装程序将会失败。此机制可防止静态版退路在无声无息的情况下取代原生 nDPI 的预期行为。

若为新安装，或希望明确以原生代理程序归档为正本，请传入 `--with-ndpi-archive PATH`。安装程序会验证归档的目标标记、拒绝不安全的 tar 路径、验证相邻的 `.sha256` 文件（如存在），并确认归档中的代理程序是否返回 `"libndpiLoaded": true`。若覆盖操作失败，将回滚整个安装。

## Configure

`routerd-dpi-classifier` 需以 `--engine auto` 或 `--engine ndpi-agent` 搭配指向代理程序 socket 的 `--ndpi-agent-socket` 进行配置。
建议使用 `auto`，以便在原生代理程序无法使用时退回至内置分类器。

已弃用的 `--ndpi-reader` 选项无法启用原生 nDPI 分类。
