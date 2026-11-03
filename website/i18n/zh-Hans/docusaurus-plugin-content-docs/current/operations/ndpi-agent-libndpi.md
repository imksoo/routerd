---
title: nDPI agent 原生软件包
---

# nDPI agent 原生软件包

routerd 1.5 提案版使用可选 Debian 软件包 `routerd-ndpi-agent-libndpi`。
Linux 本体保持静态链接；此 agent 使用 `CGO_ENABLED=1 -tags libndpi` 和 OS 的共享库。
当前发布构建目标是 Ubuntu 24.04 amd64，尚未公布公共 1.5 软件包或 apt 仓库。

取得并校验同版本的本体和附加包，使用实际文件名安装或一起更新：

```sh
sudo apt install ./routerd_<version>_amd64.deb \
  ./routerd-ndpi-agent-libndpi_<version>_amd64.deb
```

附加包依赖完全匹配的本体版本。`dpkg-shlibdeps` 从实际 ELF 计算运行库依赖，由 apt
解决。不要用归档或手动复制覆盖软件包文件。

原生 agent 仍位于 `/usr/sbin/routerd-ndpi-agent`。附加包用自己拥有的 `dpkg-divert`
将本体的静态 agent 振分至 `/usr/sbin/routerd-ndpi-agent.routerd-static`，因此本体更新
不会覆盖原生版本。拒绝接管其他软件包或管理员的 diversion。

更新会先停止自有 main 和 helper，再替换并恢复服务；否则 main 可能在替换期间
自动重启 helper。附加包的变更也可能中断通信和触发协议重新收敛。
首次安装而未启用配置时不启动服务。

```sh
/usr/sbin/routerd-ndpi-agent selftest
sudo curl --fail --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

确认 `libndpiLoaded: true`。真实库的 TLS 自测应给出预期 SNI 和
`reason: libndpi_protocol_detection`。仅安装文件不代表已验证真实转发流量分类。

```sh
sudo apt remove routerd-ndpi-agent-libndpi
```

移除附加包后，dpkg 恢复当前本体的静态 agent 并恢复此前运行的自有服务，保留配置和 DB。
静态 agent 不加载 libnDPI，所以仍需要原生分类时不要移除此包。

构建使用 `make dist-ndpi-agent-libndpi-deb ROUTERD_OS=linux GOARCH=amd64`，需要 Go、
C compiler、`libndpi-dev`、`pkg-config`、`dpkg-dev`、`file` 和 `readelf`。
隔离 OS 的安装、更新、移除及合格标准见[英文测试流程](https://routerd.net/docs/operations/ndpi-agent-libndpi#build-and-test)。
