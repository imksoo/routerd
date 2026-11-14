#!/usr/bin/env python3
"""Publish resource fields and existing YAML excerpts from the config schema/examples."""
from pathlib import Path
import json
import re

ROOT = Path(__file__).resolve().parents[1]
SCHEMA = ROOT / 'schemas/routerd-config-v1alpha1.schema.json'
VARIANTS = json.loads(SCHEMA.read_text())['properties']['spec']['properties']['resources']['items']['oneOf']
# Publication order is independent of schema registration and runtime ordering.
# Within each purpose group, use case-insensitive Kind-name order.
GROUPS = [
    (('Interfaces and addresses', 'インターフェース・アドレス', '介面與位址', '接口与地址'), 'Interface IPv4StaticAddress VirtualAddress VRF SelfAddressPolicy'),
    (('WAN and address acquisition', 'WAN接続・アドレス取得', 'WAN連線與位址取得', 'WAN连接与地址获取'), 'DHCPv4Client DHCPv6Address IPv6RAAddress DHCPv6PrefixDelegation IPv6DelegatedAddress DHCPv6Information PPPoESession DSLiteTunnel'),
    (('VPN and tunnels', 'VPN・トンネル', 'VPN與通道', 'VPN与隧道'), 'IPsecConnection WireGuardInterface WireGuardPeer TailscaleNode TunnelInterface VXLANTunnel OverlayPeer'),
    (('Routing and path health', '経路・経路監視', '路由與路徑監控', '路由与路径监控'), 'IPv4Route ClusterNetworkRoute BGPRouter BGPPeer BGPDynamicPeer BFD EgressRoutePolicy HealthCheck HybridRoute'),
    (('DHCP servers and advertisements', 'DHCP配布・RA・リース同期', 'DHCP配發、RA與租約同步', 'DHCP分配、RA与租约同步'), 'DHCPv4Server DHCPv4Reservation DHCPv4Relay DHCPv6Server IPv6RouterAdvertisement DHCPv4ServerLeaseSync DHCPv6ServerLeaseSync DHCPv6PrefixDelegationLeaseSync'),
    (('DNS', 'DNS', 'DNS', 'DNS'), 'DNSZone DNSResolver DNSForwarder DNSUpstream'),
    (('NAT and service publishing', 'NAT・サービス公開', 'NAT與服務發布', 'NAT与服务发布'), 'NAT44Rule NAT44FlowDNATPinhole NAT44SessionSync PortForward IngressService LocalServiceRedirect IPAddressSet'),
    (('Firewall and client control', 'ファイアウォール・クライアント制御', '防火牆與用戶端控制', '防火墙与客户端控制'), 'FirewallZone FirewallPolicy FirewallRule ClientPolicy FirewallFlowPinhole'),
    (('System and management', 'システム・管理', '系統與管理', '系统与管理'), 'Hostname Sysctl SysctlProfile Package NTPClient NTPServer WebConsole ControlAPI RouterdCluster ManagementAccess'),
    (('Logs and telemetry', 'ログ・監視', '日誌與監控', '日志与监控'), 'LogSink LogRetention Telemetry ObservabilityPipeline TrafficFlowLog FirewallEventLog'),
    (('Events and federation', 'イベント・イベント連携', '事件與事件聯邦', '事件与事件联邦'), 'EventRule DerivedEvent EventGroup EventPeer EventSubscription FederationSLO'),
    (('SAM and mobility', 'SAM・モビリティ', 'SAM與行動性', 'SAM与移动性'), 'MobilityPool SAMNodeSet SAMEnrollmentPolicy SAMEnrollmentClaim SAMEnrollmentClient SAMTransportProfile SAMSubnetPolicy'),
    (('Extensions and provider actions', '拡張・動的設定・プロバイダー操作', '擴充、動態設定與供應商操作', '扩展、动态配置与提供商操作'), 'Plugin DynamicConfigSource DynamicOverridePolicy CloudProviderProfile ProviderActionPolicy'),
]
BY_KIND = {v['properties']['kind']['const']: v for v in VARIANTS}
GROUP_KINDS = [(labels, sorted(names.split(), key=str.casefold)) for labels, names in GROUPS]
ordered_kinds = [kind for _, names in GROUP_KINDS for kind in names]
if len(ordered_kinds) != len(set(ordered_kinds)) or set(ordered_kinds) != set(BY_KIND):
    raise ValueError('Documentation groups must contain every schema Kind exactly once')
VARIANTS = [BY_KIND[kind] for kind in ordered_kinds]
ORDER_POLICY = [
    'Resources are grouped by purpose, then sorted by Kind name within each group. This is a reading order, not YAML evaluation, dependency or execution order.',
    '用途別に分類し、分類内はKind名順に並べています。閲覧用の順序であり、YAMLの評価順・依存順・実行順ではありません。',
    '依用途分類，分類內按Kind名稱排序。這是閱讀順序，不代表YAML評估、相依或執行順序。',
    '按用途分类，分类内按Kind名称排序。这是阅读顺序，不代表YAML求值、依赖或执行顺序。',
]
REPO = 'https://github.com/imksoo/routerd/blob/main/'
NOTES = json.loads((ROOT / 'docs/reference/resource-notes.json').read_text())
NOTE_LABELS = {
    'en': ('Conditions and references', 'Field meaning and default', 'Implementation'),
    'ja': ('指定条件と参照関係', '項目の意味と既定値', '実装'),
    'zh-Hant': ('指定條件與參照關係', '欄位意義與預設值', '實作'),
    'zh-Hans': ('指定条件与引用关系', '字段含义与默认值', '实现'),
}

# This is the same allowlist used by ordinary status-reference validation.
# Names are identifiers; availability still depends on the actual controller.
PROVIDED = {}
for kind, fields in re.findall(r'^\s*"(\w+)":\s+withCommon\((.*)\),$', (ROOT / 'pkg/api/provides.go').read_text(), re.M):
    PROVIDED[kind] = ['phase'] + re.findall(r'\b(?:s|ss|i|b|o|os|t)\("([^"]+)",', fields)
PROVIDED_LABELS = {
    'en': ('Referenceable status fields', 'The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.', 'No fields are registered for this kind in the ordinary status-reference allowlist.'),
    'ja': ('状態値の参照に使えるフィールド', '通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。', 'このKindには通常の状態値参照の許可フィールドが登録されていません。'),
    'zh-Hant': ('可參照的狀態欄位', '一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。', '此 Kind 未登錄一般狀態參照的允許欄位。'),
    'zh-Hans': ('可引用的状态字段', '常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。', '此 Kind 未登记常规状态引用的允许字段。'),
}

EXAMPLES = {}
# Only resource blocks under Router.spec.resources, not nested kinds or templates.
paths = sorted((ROOT / 'examples').glob('*.yaml'))
paths += sorted((ROOT / 'examples').glob('*/*.yaml'))
for path in paths:
    text = path.read_text()
    # Demo templates require external substitution and are not ready YAML examples.
    if '${DEMO_' in text:
        continue
    for kind in re.findall(r'^      kind: (\w+)\s*$', text, re.M):
        EXAMPLES.setdefault(kind, path)
EXAMPLES['DSLiteTunnel'] = ROOT / 'examples/example-dslite-home.yaml'
EXAMPLES['LocalServiceRedirect'] = ROOT / 'examples/example-local-dns-redirect.yaml'

LANGUAGES = {
    'en': ('Resource configuration reference', 'Example', 'Place resources inside Router.spec.resources. Names, interfaces, addresses, credentials and peer settings in examples must be adapted. A schema-required field is required within its parent object; cross-resource and semantic constraints are checked by routerd validate, not by the schema alone.'),
    'ja': ('リソース別の設定', '設定例', '各リソースはRouter.spec.resourcesに記述します。例の名前・インターフェース・アドレス・認証情報・対向設定は環境に合わせて変更してください。必須は親オブジェクト内での指定要否です。リソース間参照や意味上の制約はスキーマだけでは確定せず、routerd validateで確認します。'),
    'zh-Hant': ('各資源的設定', '設定範例', '資源寫在Router.spec.resources內。範例的名稱、介面、位址、憑證及對端設定必須依環境調整。必填表示在所屬物件內必須指定。跨資源參照與語意限制還須以routerd validate確認。'),
    'zh-Hans': ('各资源的配置', '配置示例', '资源写在Router.spec.resources内。示例中的名称、接口、地址、凭据及对端配置必须按环境调整。必填表示在所属对象内必须指定。跨资源引用与语义限制还须通过routerd validate确认。'),
}


DEFINITIONS = {
    'en': ('Configuration syntax', 'This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.'),
    'ja': ('設定構文', '説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。'),
    'zh-Hant': ('設定語法', '這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。'),
    'zh-Hans': ('配置语法', '这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。'),
}
PRESENCE_LABELS = {
    'en': ('Presence', 'Type / constraints', 'Required', 'Conditionally required', 'Optional', 'Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.'),
    'ja': ('指定要否', '型・制約', '必須', '条件付き必須', '任意', '任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。'),
    'zh-Hant': ('指定要求', '型別與限制', '必填', '條件必填', '選填', '選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。'),
    'zh-Hans': ('指定要求', '类型与限制', '必填', '条件必填', '选填', '选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。'),
}


def annotation(node):
    parts = [node.get('type', 'any')]
    for key in ['enum', 'default', 'minimum', 'maximum', 'minItems', 'minLength', 'pattern']:
        if key in node:
            parts.append(key + '=' + json.dumps(node[key], ensure_ascii=False))
    return '; '.join(str(part) for part in parts)


def scalar(node):
    if 'const' in node:
        return json.dumps(node['const'], ensure_ascii=False)
    if 'enum' in node:
        return '(' + ' | '.join(json.dumps(v, ensure_ascii=False) for v in node['enum']) + ')'
    return '<' + node.get('type', 'value') + '>'


def properties(node):
    props = node.get('properties', {})
    if not props and isinstance(node.get('additionalProperties'), dict):
        return {'<name>': node['additionalProperties']}
    if not props and node.get('additionalProperties') is True:
        return {'<name>': {'type': 'value'}}
    return props


def locally_required(node, name, path, notes):
    if path in notes.get('conditionalFields', []):
        return False
    if path in notes.get('requiredFields', []):
        return True
    props = properties(node)
    if name == 'field' and 'resource' in props and set(props) <= {'resource', 'field', 'optional'}:
        return True
    return name in node.get('required', [])


def syntax_fields(node, notes, indent=0, path=''):
    for name, value in properties(node).items():
        field = path + '.' + name if path else name
        optional = not locally_required(node, name, field, notes)
        lines = []
        kind = value.get('type', 'object')
        child_indent = indent + (4 if optional else 2)
        if kind == 'object':
            children = list(syntax_fields(value, notes, child_indent, field))
            lines = [name + (':' if children else ': {}')] + children
        elif kind == 'array':
            lines = [name + ':'] + list(syntax_items(value.get('items', {}), notes, child_indent, field + '[]'))
        else:
            lines = [name + ': ' + scalar(value)]
        prefix = ' ' * indent
        if optional:
            if len(lines) == 1:
                yield prefix + '[ ' + lines[0] + ' ]'
            else:
                yield prefix + '[ ' + lines[0]
                yield from lines[1:]
                yield prefix + ']'
        else:
            yield prefix + lines[0]
            yield from lines[1:]


def syntax_items(node, notes, indent, path):
    kind = node.get('type', 'value')
    if kind == 'object':
        yield ' ' * indent + '-'
        yield from syntax_fields(node, notes, indent + 2, path)
    elif kind == 'array':
        yield ' ' * indent + '-'
        yield from syntax_items(node.get('items', {}), notes, indent + 2, path + '[]')
    else:
        yield ' ' * indent + '- ' + scalar(node)


def field_nodes(node, notes, path='', optional_parent=False):
    result = {}
    for name, value in properties(node).items():
        field = path + '.' + name if path else name
        required = locally_required(node, name, field, notes)
        condition = field in notes.get('conditionalFields', []) or (required and optional_parent)
        result[field] = (value, 'conditional' if condition else 'required' if required else 'optional')
        child, childpath = value, field
        while child.get('type') == 'array':
            child = child.get('items', {})
            childpath += '[]'
            result[childpath] = (child, 'conditional')
        result.update(field_nodes(child, notes, childpath, optional_parent or not required))
    return result


for locale_index, (locale, labels) in enumerate(LANGUAGES.items()):
    title, example, intro = labels
    definition, guidance = DEFINITIONS[locale]
    conditions, field_notes, implementation = NOTE_LABELS[locale]
    base = ROOT / 'docs' if locale == 'en' else ROOT / 'docs' / locale
    lines = ['# ' + title, '', intro, '', '[Kind/name · when · dependsOn](common-fields.md)', '', '```sh', 'routerd validate --config router.yaml', '```', '', '[JSON Schema](' + REPO + 'schemas/routerd-config-v1alpha1.schema.json) · [YAML](' + REPO.replace('/blob/', '/tree/') + 'examples)', '', '### Router', '', '```yaml', 'apiVersion: routerd.net/v1alpha1', 'kind: Router', 'metadata:', '  name: example', 'spec:', '  resources:', '    - apiVersion: net.routerd.net/v1alpha1', '      kind: Interface', '      metadata:', '        name: lan', '      spec:', '        ifname: ens18', '        managed: true', '```', '']
    lines += [ORDER_POLICY[locale_index], '']
    for group_labels, names in GROUP_KINDS:
        lines += ['## ' + group_labels[locale_index], '']
        for kind in names:
            lines += ['### ' + kind, '', '[' + kind + '](resources/' + kind + '.md)', '', NOTES[kind]['purpose'][locale_index], '']
    for variant in VARIANTS:
        props = variant['properties']; kind = props['kind']['const']
        notes = NOTES[kind]
        page = ['# ' + kind, '', '[' + title + '](../resources.md#' + kind.lower() + ') · [Kind/name · when · dependsOn](../common-fields.md)', '', '`apiVersion: ' + props['apiVersion']['const'] + '` · `metadata.name`', '']
        if notes:
            page += [notes['purpose'][locale_index], '', '## ' + conditions, '']
            page += ['- ' + rule[locale_index] for rule in notes['rules']]
            page += ['']
        page += ['## ' + definition, '', guidance, '', '```text', '- apiVersion: ' + props['apiVersion']['const'], '  kind: ' + kind, '  metadata:', '    name: "<name>"', '  spec:']
        page += list(syntax_fields(props['spec'], notes, 4))
        page += ['```', '']
        if notes:
            presence, type_label, req, cond, opt, legend = PRESENCE_LABELS[locale]
            nodes = field_nodes(props['spec'], notes)
            page += ['## ' + field_notes, '', legend, '', '| YAML | ' + presence + ' | ' + type_label + ' | ' + field_notes + ' |', '| --- | --- | --- | --- |']
            for path, text in notes['fields'].items():
                node, status = nodes[path]
                marker = '**' + (req if status == 'required' else cond) + '**' if status != 'optional' else '*' + opt + '*'
                constraint = annotation(node).replace('|', '&#124;') if node else '—'
                page.append('| `spec.' + path + '` | ' + marker + ' | ' + constraint + ' | ' + text[locale_index].replace('|', '&#124;') + ' |')
            page += ['', implementation + ': ' + ' · '.join('[' + path + '](' + REPO + path + ')' for path in notes['sources']), '']
        provided_title, provided_intro, provided_empty = PROVIDED_LABELS[locale]
        page += ['## ' + provided_title, '', provided_intro, '']
        page += [', '.join('`' + field + '`' for field in PROVIDED[kind]) if kind in PROVIDED else provided_empty, '']
        if kind in EXAMPLES:
            path = EXAMPLES[kind]
            page += ['[' + example + '](' + REPO + path.relative_to(ROOT).as_posix() + ')', '']
        target = base / 'reference/resources' / (kind + '.md')
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text('\n'.join(page).rstrip() + '\n')
    target = base / 'reference/resources.md'
    target.write_text('\n'.join(lines).rstrip() + '\n')
