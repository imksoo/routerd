---
title: VS Code YAML schema
slug: /how-to/vscode-yaml-schema
---

# VS Code YAML schema

routerd 在以下 URL 發布產生的 config schema：

```text
https://routerd.net/schemas/routerd-config-v1alpha1.schema.json
```

## 每個檔案的 modeline

在任意 routerd config 檔案開頭放置此註解：

```yaml
# yaml-language-server: $schema=https://routerd.net/schemas/routerd-config-v1alpha1.schema.json
apiVersion: routerd.net/v1alpha1
kind: Router
```

VS Code 與 YAML extension 會讀取該 modeline，並為該檔案啟用補全、hover、
enum 驗證和型別診斷。

## workspace mapping

本 repository 包含 `.vscode/settings.json`，其中為 router config 檔案設定了
`yaml.schemas` mapping：

- `examples/*.yaml`
- `examples/cloudedge-mobility-demo/*.yaml`
- `examples/event-federation/*.yaml`
- `website/fixtures/wizard/**/*.yaml`
- `routerd/**/*.yaml`
- `*.routerd.yaml`
- `**/routerd.yaml`
- `**/router.yaml`

在其他 workspace 中，可將相同 mapping 複製到該 workspace 的 settings 檔案：

```json
{
  "yaml.schemas": {
    "https://routerd.net/schemas/routerd-config-v1alpha1.schema.json": [
      "examples/*.yaml",
      "examples/cloudedge-mobility-demo/*.yaml",
      "examples/event-federation/*.yaml",
      "website/fixtures/wizard/**/*.yaml",
      "routerd/**/*.yaml",
      "*.routerd.yaml",
      "**/routerd.yaml",
      "**/router.yaml"
    ]
  }
}
```

對於不符合這些 pattern 的任意 YAML 檔案，請使用 modeline。

## 檢查

schema 從 Go API types 產生。CI 會檢查 repository 中的 schema 與 website 發布副本
保持同步，因此 editor feedback 會跟隨 `routerd validate` 使用的同一份契約。
