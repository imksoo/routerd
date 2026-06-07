---
title: VS Code YAML schema
slug: /how-to/vscode-yaml-schema
---

# VS Code YAML schema

routerd は生成済み config schema を次の URL で公開しています。

```text
https://routerd.net/schemas/routerd-config-v1alpha1.schema.json
```

## ファイルごとの modeline

任意の routerd config file の先頭にこの comment を置きます。

```yaml
# yaml-language-server: $schema=https://routerd.net/schemas/routerd-config-v1alpha1.schema.json
apiVersion: routerd.net/v1alpha1
kind: Router
```

VS Code と YAML extension はこの modeline を読み、該当 file で補完、hover、
enum 検証、型診断を有効にします。

## workspace mapping

この repository には、router config file 向けの `yaml.schemas` mapping を持つ
`.vscode/settings.json` が入っています。

- `examples/*.yaml`
- `examples/cloudedge-mobility-demo/*.yaml`
- `examples/event-federation/*.yaml`
- `website/fixtures/wizard/**/*.yaml`
- `routerd/**/*.yaml`
- `*.routerd.yaml`
- `**/routerd.yaml`
- `**/router.yaml`

別 workspace では、同じ mapping をその workspace の settings に入れてください。

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

これらの pattern に合わない YAML filename では modeline を使ってください。

## checks

schema は Go API type から生成されます。CI は repository 内の schema と website
で公開する copy が同期していることを確認します。そのため editor feedback は
`routerd validate` が使う契約と同じものに追従します。
