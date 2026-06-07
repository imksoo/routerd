---
title: 貢獻
---

# 貢獻

![Diagram showing the routerd contribution flow from host-facing change scope through local checks, schema checks, example validation, website build, shellcheck, and license agreement](/img/diagrams/contributing.png)

routerd 是預發布階段的路由器控制平面軟體。
我們歡迎各種貢獻。
但涉及網路、防火牆、路由、安裝程式或主機服務管理的變更，
需要謹慎審查。

正式的貢獻指南位於 repository root：

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## 本機確認

開啟 pull request 之前，請執行下列指令：

```sh
make test
make check-schema
make validate-example
make website-build
```

若有修改 shell script，也請對修改的 script 執行 `shellcheck`。

## pre-commit hook

routerd 提供可選用的 pre-commit hook：

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

這個 hook 會在 commit 前執行 Go 測試與 schema 確認。

## 文件的文體與術語

撰寫文件時，請遵循以下文體規範。頁面之間用語不一致會造成讀者混淆。

- 文體以**正式書面語**統一。
- 以**一句一義**為原則，較長的句子請拆分。
- **避免被動語態**，改以主動語態書寫（例：「設定被套用」→「routerd 套用設定」）。
- 專業術語依照[詞彙表](concepts/glossary.md)使用，**首次出現時**以「中文（English）」格式附上英文對照。
- 指令、欄位名稱、`Kind` 等識別字保留原文（英數字），以 code span 標示。

若需新增或修改譯詞，請先更新[詞彙表](concepts/glossary.md)，再於各頁面套用。

## 授權條款

routerd 以 BSD 3-Clause License 發布。

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

向本 repository 提交貢獻，即表示您同意該貢獻以相同授權條款提供，除非檔案中另有明確說明。
