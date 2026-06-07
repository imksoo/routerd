---
title: 贡献
---

# 贡献

![Diagram showing the routerd contribution flow from host-facing change scope through local checks, schema checks, example validation, website build, shellcheck, and license agreement](/img/diagrams/contributing.png)

routerd 是预发布阶段的路由器控制平面软件。
我们欢迎各种贡献。
但涉及网络、防火墙、路由、安装程序或主机服务管理的变更，
需要谨慎审查。

正式的贡献指南位于 repository root：

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## 本地确认

打开 pull request 之前，请执行下列命令：

```sh
make test
make check-schema
make validate-example
make website-build
```

若有修改 shell script，也请对修改的 script 执行 `shellcheck`。

## pre-commit hook

routerd 提供可选用的 pre-commit hook：

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

这个 hook 会在 commit 前执行 Go 测试与 schema 确认。

## 文档的文体与术语

撰写文档时，请遵循以下文体规范。页面之间用语不一致会造成读者混淆。

- 文体以**正式书面语**统一。
- 以**一句一义**为原则，较长的句子请拆分。
- **避免被动语态**，改以主动语态书写（例：「配置被应用」→「routerd 应用配置」）。
- 专业术语依照[词汇表](concepts/glossary.md)使用，**首次出现时**以「中文（English）」格式附上英文对照。
- 命令、字段名称、`Kind` 等标识符保留原文（英数字），以 code span 标示。

若需新增或修改译词，请先更新[词汇表](concepts/glossary.md)，再于各页面应用。

## 授权条款

routerd 以 BSD 3-Clause License 发布。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

向本 repository 提交贡献，即表示您同意该贡献以相同授权条款提供，除非文件中另有明确说明。
