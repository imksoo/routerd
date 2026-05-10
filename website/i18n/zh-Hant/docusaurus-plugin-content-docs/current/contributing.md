---
title: 貢獻
---

# 貢獻

routerd 是預發布的路由器控制平面軟體。我們歡迎貢獻。
但涉及網路、防火牆、路由、安裝器或主機服務管理的變更需要謹慎 review。

正式貢獻指南位於 repository root：

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## 本地檢查

打開 pull request 之前，請執行：

```sh
make test
make check-schema
make validate-example
make website-build
```

如果修改了 shell scripts，也請對修改的 scripts 執行 `shellcheck`。

## pre-commit hook

routerd 提供可選的 pre-commit hook：

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

這個 hook 會在 commit 前執行 Go tests 和 schema checks。

## License

routerd 以 BSD 3-Clause License 發布：

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

向本 repository 貢獻程式碼，即表示你同意貢獻內容使用相同 license，除非檔案中另有說明。
