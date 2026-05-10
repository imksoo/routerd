---
title: 贡献
---

# 贡献

routerd 是预发布的路由器控制平面软件。我们欢迎贡献。
但涉及网络、防火墙、路由、安装器或主机服务管理的变更需要谨慎 review。

正式贡献指南位于 repository root：

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## 本地检查

打开 pull request 之前，请运行：

```sh
make test
make check-schema
make validate-example
make website-build
```

如果修改了 shell scripts，也请对修改的 scripts 运行 `shellcheck`。

## pre-commit hook

routerd 提供可选的 pre-commit hook：

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

这个 hook 会在 commit 前运行 Go tests 和 schema checks。

## License

routerd 以 BSD 3-Clause License 发布：

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

向本 repository 贡献代码，即表示你同意贡献内容使用相同 license，除非文件中另有说明。
