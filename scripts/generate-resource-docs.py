#!/usr/bin/env python3
"""Publish resource fields and existing YAML excerpts from the config schema/examples."""
from pathlib import Path
import json
import re

ROOT = Path(__file__).resolve().parents[1]
SCHEMA = ROOT / 'schemas/routerd-config-v1alpha1.schema.json'
VARIANTS = json.loads(SCHEMA.read_text())['properties']['spec']['properties']['resources']['items']['oneOf']
REPO = 'https://github.com/imksoo/routerd/blob/main/'
NOTES = json.loads((ROOT / 'docs/reference/resource-notes.json').read_text())
NOTE_LABELS = {
    'en': ('Conditions and references', 'Field meaning and default', 'Implementation'),
    'ja': ('指定条件と参照関係', '項目の意味と既定値', '実装'),
    'zh-Hant': ('指定條件與參照關係', '欄位意義與預設值', '實作'),
    'zh-Hans': ('指定条件与引用关系', '字段含义与默认值', '实现'),
}

EXAMPLES = {}
# Only resource blocks under Router.spec.resources, not nested kinds or templates.
paths = sorted((ROOT / 'examples').glob('*.yaml'))
paths += sorted((ROOT / 'examples').glob('*/*.yaml'))
for path in paths:
    for kind in re.findall(r'^      kind: (\w+)\s*$', path.read_text(), re.M):
        EXAMPLES.setdefault(kind, path)
EXAMPLES['DSLiteTunnel'] = ROOT / 'examples/example-dslite-home.yaml'
EXAMPLES['LocalServiceRedirect'] = ROOT / 'examples/example-local-dns-redirect.yaml'

LANGUAGES = {
    'en': ('Resource configuration reference', 'Required', 'Example', 'Place resources inside Router.spec.resources. Names, interfaces, addresses, credentials and peer settings in examples must be adapted. A schema-required field is required within its parent object; cross-resource and semantic constraints are checked by routerd validate, not by the schema alone.'),
    'ja': ('リソース別の設定', '必須', '設定例', '各リソースはRouter.spec.resourcesに記述します。例の名前・インターフェース・アドレス・認証情報・対向設定は環境に合わせて変更してください。必須は親オブジェクト内での指定要否です。リソース間参照や意味上の制約はスキーマだけでは確定せず、routerd validateで確認します。'),
    'zh-Hant': ('各資源的設定', '必填', '設定範例', '資源寫在Router.spec.resources內。範例的名稱、介面、位址、憑證及對端設定必須依環境調整。必填表示在所屬物件內必須指定。跨資源參照與語意限制還須以routerd validate確認。'),
    'zh-Hans': ('各资源的配置', '必填', '配置示例', '资源写在Router.spec.resources内。示例中的名称、接口、地址、凭据及对端配置必须按环境调整。必填表示在所属对象内必须指定。跨资源引用与语义限制还须通过routerd validate确认。'),
}


DEFINITIONS = {
    'en': ('YAML definition', 'optional', 'All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.'),
    'ja': ('YAML定義', '任意', '全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。'),
    'zh-Hant': ('YAML定義', '選填', '此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。'),
    'zh-Hans': ('YAML定义', '选填', '此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。'),
}


def annotation(node):
    parts = [node.get('type', 'any')]
    for key in ['enum', 'default', 'minimum', 'maximum', 'minItems', 'minLength', 'pattern']:
        if key in node:
            parts.append(key + '=' + json.dumps(node[key], ensure_ascii=False))
    return '; '.join(str(part) for part in parts)


def scalar(node):
    if 'const' in node:
        value = node['const']
    elif 'default' in node:
        value = node['default']
    elif 'enum' in node:
        value = node['enum'][0]
    else:
        value = {'boolean': True, 'integer': node.get('minimum', 0), 'number': node.get('minimum', 0), 'null': None}.get(node.get('type'), '<value>' if node.get('type') == 'any' else '<string>')
    return json.dumps(value, ensure_ascii=False)


def yaml_fields(node, required_label, optional_label, indent=0):
    properties = node.get('properties', {})
    if not properties and isinstance(node.get('additionalProperties'), dict):
        properties = {'<key>': node['additionalProperties']}
    elif not properties and node.get('additionalProperties') is True:
        properties = {'<key>': {'type': 'any'}}
    for name, value in properties.items():
        key = json.dumps(name) if name.startswith('<') else name
        comment = required_label if name in node.get('required', []) else optional_label
        comment += '; ' + annotation(value)
        prefix = ' ' * indent + key + ':'
        kind = value.get('type', 'object')
        if kind == 'object':
            children = list(yaml_fields(value, required_label, optional_label, indent + 2))
            yield prefix + (' ' if children else ' {} ') + '# ' + comment
            yield from children
        elif kind == 'array':
            yield prefix + ' # ' + comment
            yield from yaml_items(value.get('items', {}), required_label, optional_label, indent + 2)
        else:
            yield prefix + ' ' + scalar(value) + ' # ' + comment


def yaml_items(node, required_label, optional_label, indent):
    prefix = ' ' * indent + '-'
    kind = node.get('type', 'any')
    if kind == 'object':
        yield prefix + ' # object'
        children = list(yaml_fields(node, required_label, optional_label, indent + 2))
        yield from children or [' ' * (indent + 2) + '{}']
    elif kind == 'array':
        yield prefix + ' # array'
        yield from yaml_items(node.get('items', {}), required_label, optional_label, indent + 2)
    else:
        yield prefix + ' ' + scalar(node) + ' # ' + annotation(node)


for locale_index, (locale, labels) in enumerate(LANGUAGES.items()):
    title, required, example, intro = labels
    definition, optional, guidance = DEFINITIONS[locale]
    conditions, field_notes, implementation = NOTE_LABELS[locale]
    base = ROOT / 'docs' if locale == 'en' else ROOT / 'docs' / locale
    lines = ['# ' + title, '', intro, '', '[Kind/name · when · dependsOn](common-fields.md)', '', '```sh', 'routerd validate --config router.yaml', '```', '', '[JSON Schema](' + REPO + 'schemas/routerd-config-v1alpha1.schema.json) · [YAML](' + REPO.replace('/blob/', '/tree/') + 'examples)', '', '### Router', '', '```yaml', 'apiVersion: routerd.net/v1alpha1', 'kind: Router', 'metadata:', '  name: "<router-name>"', 'spec:', '  resources:', '    - apiVersion: net.routerd.net/v1alpha1', '      kind: Interface', '      metadata:', '        name: "<name>"', '      spec:', '        ifname: "<interface-name>"', '```', '']
    for variant in VARIANTS:
        kind = variant['properties']['kind']['const']
        lines.append('- [' + kind + '](resources/' + kind + '.md)')
    for variant in VARIANTS:
        props = variant['properties']; kind = props['kind']['const']
        notes = NOTES.get(kind)
        lines += ['', '## ' + kind, '', '[' + kind + '](resources/' + kind + '.md)', '']
        page = ['# ' + kind, '', '[' + title + '](../resources.md#' + kind.lower() + ') · [Kind/name · when · dependsOn](../common-fields.md)', '', '`apiVersion: ' + props['apiVersion']['const'] + '` · `metadata.name`', '']
        if notes:
            lines += [notes['purpose'][locale_index], '']
            page += [notes['purpose'][locale_index], '', '## ' + conditions, '']
            page += ['- ' + rule[locale_index] for rule in notes['rules']]
            page += ['']
        page += ['## ' + definition, '', guidance, '', '```yaml', '- apiVersion: ' + props['apiVersion']['const'], '  kind: ' + kind, '  metadata:', '    name: "<name>"', '  spec:']
        page += list(yaml_fields(props['spec'], required, optional, 4))
        page += ['```', '']
        if notes:
            page += ['## ' + field_notes, '', '| YAML | ' + field_notes + ' |', '| --- | --- |']
            for path, text in notes['fields'].items():
                page.append('| `spec.' + path + '` | ' + text[locale_index].replace('|', '&#124;') + ' |')
            page += ['', implementation + ': ' + ' · '.join('[' + path + '](' + REPO + path + ')' for path in notes['sources']), '']
        if kind in EXAMPLES:
            path = EXAMPLES[kind]
            page += ['[' + example + '](' + REPO + path.relative_to(ROOT).as_posix() + ')', '']
        target = base / 'reference/resources' / (kind + '.md')
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text('\n'.join(page).rstrip() + '\n')
    target = base / 'reference/resources.md'
    target.write_text('\n'.join(lines).rstrip() + '\n')
