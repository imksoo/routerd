// SPDX-License-Identifier: BSD-3-Clause

import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

const STABLE_VERSION = 'v20260707.1514';

interface ScenarioCard {
  title: string;
  problem: string;
  link: string;
  linkLabel: string;
  resources: string[];
}

interface OperatorStep {
  number: number;
  label: string;
  command: string;
}

interface ObservabilityItem {
  title: string;
  description: string;
}

interface QuickstartStage {
  title: string;
  body: string;
  command: string;
}

interface SiteCopy {
  title: string;
  description: string;
  eyebrow: string;
  headline: string;
  subtitle: string;
  tutorial: string;
  resources: string;
  resourceModel: string;
  stable: string;
  operatorLoopTitle: string;
  operatorLoopSubtitle: string;
  operatorSteps: OperatorStep[];
  scenariosTitle: string;
  scenarios: ScenarioCard[];
  observabilityTitle: string;
  observabilitySubtitle: string;
  observabilityItems: ObservabilityItem[];
  quickstartTitle: string;
  quickstartBody: string;
  installLinkLabel: string;
  quickstartStages: QuickstartStage[];
  note: string;
}

type SupportedHomepageLocale = 'en' | 'ja' | 'zh-Hans' | 'zh-Hant';

const copy: Record<SupportedHomepageLocale, SiteCopy> = {
  en: {
    title: 'Ubuntu Server router control, declared in YAML',
    description:
      'routerd applies typed YAML resources to an Ubuntu Server router. FreeBSD and NixOS currently have install and service-manager groundwork; native platform renderers are still pending.',
    eyebrow: 'Start in an isolated Ubuntu Server VM',
    headline: 'Describe the router first. Change the network only when you are ready.',
    subtitle:
      'routerd makes WAN, LAN, DNS, NAT, routes, and system settings explicit in one YAML file. Start with a lab host and keep a console or separate management interface: a live run can change connectivity.',
    tutorial: 'Start on Ubuntu Server',
    resources: 'Browse configuration',
    resourceModel: 'How resources work',
    stable: 'Latest stable',
    operatorLoopTitle: 'Then make the first safe change',
    operatorLoopSubtitle:
      'Follow this order. Validation and dry-run do not change the host network. The console apply and daemon can; use a console or independent management path. Use routerctl only after the daemon is running.',
    operatorSteps: [
      {number: 1, label: 'Learn the basics', command: 'WAN / LAN / DHCP / DNS'},
      {number: 2, label: 'Prepare an Ubuntu lab', command: 'console / management NIC'},
      {number: 3, label: 'Write YAML', command: 'router.yaml'},
      {number: 4, label: 'Validate only', command: 'routerd validate'},
      {number: 5, label: 'Isolated dry-run', command: 'routerd apply --once --dry-run'},
      {number: 6, label: 'Console apply (live)', command: 'routerd apply --once'},
      {number: 7, label: 'Daemon, then inspect', command: 'routerd serve → routerctl'},
    ],
    scenariosTitle: 'Begin here, in this order',
    scenarios: [
      {
        title: 'Learn six network words',
        problem:
          'You only need WAN, LAN, IP address, gateway, DHCP, and DNS for the first lab. The guide explains them without assuming networking experience.',
        link: '/docs/tutorials/network-basics',
        linkLabel: 'Learn the six basics →',
        resources: ['WAN', 'LAN', 'DHCP', 'DNS'],
      },
      {
        title: 'Prepare an isolated Ubuntu Server lab',
        problem:
          'Use a spare VM or host with console access or a separate management NIC. Do not make the first live change on a production or only router.',
        link: '/docs/tutorials/getting-started',
        linkLabel: 'Set up the lab →',
        resources: ['Ubuntu Server', 'console access', 'separate management NIC'],
      },
      {
        title: 'Write one small router job',
        problem:
          'Start with interfaces and a narrow LAN service, then add DHCP, DNS, and outbound IPv4 NAT one responsibility at a time.',
        link: '/docs/tutorials/first-router',
        linkLabel: 'Build the first router →',
        resources: ['Interface', 'DHCPv4Server', 'NAT44Rule'],
      },
      {
        title: 'FreeBSD and NixOS: groundwork',
        problem:
          'Install layout and service-manager integration scaffolding exist, but native platform renderers and feature parity are still pending. Start your first router on Ubuntu Server.',
        link: '/docs/platforms',
        linkLabel: 'See platform status →',
        resources: ['Ubuntu Server primary', 'FreeBSD groundwork', 'NixOS groundwork'],
      },
    ],
    observabilityTitle: 'After the daemon starts, check the router',
    observabilitySubtitle:
      'routerctl talks to the running local routerd daemon. It is for checking an already-running router, not for the standalone validation and dry-run steps above.',
    observabilityItems: [
      {
        title: 'routerctl get status',
        description:
          'Check the resource phases reported by the running daemon before changing anything else.',
      },
      {
        title: 'routerctl doctor',
        description:
          'Run focused health diagnostics through the daemon after the router is up.',
      },
      {
        title: 'routerctl get events',
        description:
          'Review recent controller events when a resource does not reach the expected phase.',
      },
      {
        title: 'Keep the host console available',
        description:
          'A console or separate management path is the recovery path if a live network change affects remote access.',
      },
    ],
    quickstartTitle: 'Ubuntu Server: check first, then go live',
    quickstartBody:
      'Install routerd on a spare Ubuntu Server VM or host, place the sample YAML at the path below, and keep a console or independent management path available before the live step.',
    installLinkLabel: 'Read the Ubuntu Server installation guide',
    quickstartStages: [
      {
        title: '1. Validate the file only',
        body: 'This checks YAML and resource rules. It does not change the host network and does not need routerd to be running.',
        command: 'sudo routerd validate --config /usr/local/etc/routerd/router.yaml',
      },
      {
        title: '2. Run an isolated dry-run',
        body: 'This uses temporary state, ledger, and status paths while it exercises the one-shot apply path. It does not apply network changes or write routerd’s normal state files; no daemon or routerctl command is involved.',
        command: 'LAB_DIR="$(mktemp -d)"\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager --state-file "$LAB_DIR/state.db" --ledger-file "$LAB_DIR/ledger.db" --status-file "$LAB_DIR/status.json"',
      },
      {
        title: '3. Apply live from the console',
        body: 'This changes the host network and then exits. Run it only from the lab console or with an independent management path available.',
        command: '# Live one-shot apply: changes the host network, then exits.\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once',
      },
      {
        title: '4. Start the daemon; then use routerctl',
        body: 'routerd serve is live and continues to reconcile the host network. After it starts, use another terminal for routerctl.',
        command: '# Start the live daemon from the console.\nsudo routerd serve --config /usr/local/etc/routerd/router.yaml\n\n# Only after routerd serve is running, use another terminal.\nsudo routerctl get status\nsudo routerctl get events --limit 20',
      },
    ],
    note:
      'routerd is pre-release v1alpha1 software. Do not use the first live run on the only router or only remote-management path for a network.',
  },
  ja: {
    title: 'Ubuntu Server 向け宣言的ルーター制御',
    description:
      'routerd は型付き YAML リソースを Ubuntu Server ルーターへ反映します。FreeBSD と NixOS は現在、導入とサービス管理の基盤整備の段階であり、ネイティブなプラットフォームレンダラーは未対応です。',
    eyebrow: '最初は隔離した Ubuntu Server VM で試す',
    headline: 'YAML に書いてから、ネットワークを変える。',
    subtitle:
      'routerd は WAN、LAN、DNS、NAT、経路、OS 設定を1つの YAML に明示します。最初は検証用ホストを使い、コンソールまたは独立した管理 NIC を確保してください。live 実行は接続性を変えることがあります。',
    tutorial: 'Ubuntu Server ではじめる',
    resources: '設定項目を見る',
    resourceModel: 'リソースの仕組み',
    stable: '最新安定版',
    operatorLoopTitle: '次に、安全な初回変更へ',
    operatorLoopSubtitle:
      'この順で進めてください。検証と dry-run はホストネットワークを変更しません。コンソールでの live apply とデーモンは変更する可能性があるため、コンソールまたは独立した管理経路を使います。routerctl はデーモン起動後にだけ使います。',
    operatorSteps: [
      {number: 1, label: '6つの基本用語を知る', command: 'WAN / LAN / DHCP / DNS'},
      {number: 2, label: 'Ubuntu ラボを用意', command: 'コンソール / 管理 NIC'},
      {number: 3, label: 'YAML を書く', command: 'router.yaml'},
      {number: 4, label: '静的に検証', command: 'routerd validate'},
      {number: 5, label: '隔離 dry-run', command: 'routerd apply --once --dry-run'},
      {number: 6, label: 'コンソールで live apply', command: 'routerd apply --once'},
      {number: 7, label: 'デーモン起動後に確認', command: 'routerd serve → routerctl'},
    ],
    scenariosTitle: 'この順で始める',
    scenarios: [
      {
        title: '最初に6つのネットワーク用語',
        problem:
          '最初のラボに必要なのは WAN、LAN、IP アドレス、ゲートウェイ、DHCP、DNS だけです。ネットワーク経験を前提にせず、短く説明します。',
        link: '/docs/tutorials/network-basics',
        linkLabel: '6つの基本用語を読む →',
        resources: ['WAN', 'LAN', 'DHCP', 'DNS'],
      },
      {
        title: '隔離した Ubuntu Server ラボを用意',
        problem:
          '予備の VM またはホストを使い、コンソールか独立した管理 NIC を確保します。最初の live 変更を本番や唯一のルーターに行わないでください。',
        link: '/docs/tutorials/getting-started',
        linkLabel: 'ラボを準備する →',
        resources: ['Ubuntu Server', 'コンソール接続', '独立した管理 NIC'],
      },
      {
        title: '小さなルーターの役割を1つ書く',
        problem:
          '最初はインターフェースと狭い LAN サービスだけにし、DHCP、DNS、外向き IPv4 NAT を役割ごとに1つずつ追加します。',
        link: '/docs/tutorials/first-router',
        linkLabel: '最初のルーターを作る →',
        resources: ['Interface', 'DHCPv4Server', 'NAT44Rule'],
      },
      {
        title: 'FreeBSD / NixOS は基盤整備の段階',
        problem:
          '導入レイアウトとサービス管理の土台はありますが、ネイティブなプラットフォームレンダラーと機能同等性は未対応です。最初のルーターは Ubuntu Server で始めてください。',
        link: '/docs/platforms',
        linkLabel: '対応状況を見る →',
        resources: ['Ubuntu Server が主対象', 'FreeBSD の基盤整備', 'NixOS の基盤整備'],
      },
    ],
    observabilityTitle: 'デーモン起動後にルーターを確認する',
    observabilitySubtitle:
      'routerctl はローカルで動作中の routerd デーモンと通信します。上の単体検証や dry-run に使うものではなく、起動済みルーターを確認するためのものです。',
    observabilityItems: [
      {
        title: 'routerctl get status',
        description:
          '次の変更を行う前に、実行中のデーモンが報告するリソース状態を確認します。',
      },
      {
        title: 'routerctl doctor',
        description:
          'ルーター起動後、デーモン経由で絞り込んだヘルス診断を実行します。',
      },
      {
        title: 'routerctl get events',
        description:
          'リソースが期待した状態にならないとき、最近のコントローラーイベントを確認します。',
      },
      {
        title: 'ホストコンソールを残す',
        description:
          'live のネットワーク変更でリモート接続に影響したとき、コンソールまたは独立した管理経路が復旧手段になります。',
      },
    ],
    quickstartTitle: 'Ubuntu Server: 確認してから live にする',
    quickstartBody:
      '予備の Ubuntu Server VM またはホストへ routerd を導入し、下記パスへサンプル YAML を置きます。live 実行の前に、コンソールまたは独立した管理経路を必ず確保してください。',
    installLinkLabel: 'Ubuntu Server の導入手順を読む',
    quickstartStages: [
      {
        title: '1. 設定ファイルだけを検証する',
        body: 'YAML とリソースの規則を確認します。ホストネットワークは変更せず、routerd デーモンの起動も不要です。',
        command: 'sudo routerd validate --config /usr/local/etc/routerd/router.yaml',
      },
      {
        title: '2. 隔離した dry-run を実行する',
        body: '一時ディレクトリへ state、ledger、status を出して、1回分の apply 経路を確認します。ネットワーク変更や通常の routerd 状態ファイルは書かれず、routerd デーモンも routerctl も使いません。',
        command: 'LAB_DIR="$(mktemp -d)"\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager --state-file "$LAB_DIR/state.db" --ledger-file "$LAB_DIR/ledger.db" --status-file "$LAB_DIR/status.json"',
      },
      {
        title: '3. コンソールから live apply する',
        body: 'ホストネットワークを変更して終了します。ラボのコンソール、または独立した管理経路があるときだけ実行してください。',
        command: '# 1回だけ live で反映します。ホストネットワークを変更して終了します。\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once',
      },
      {
        title: '4. デーモンを起動してから routerctl を使う',
        body: 'routerd serve は live であり、実行中もホストネットワークを反映・調整します。起動後にだけ、別の端末で routerctl を使います。',
        command: '# コンソールから live デーモンを起動します。\nsudo routerd serve --config /usr/local/etc/routerd/router.yaml\n\n# routerd serve の起動後、別の端末で実行します。\nsudo routerctl get status\nsudo routerctl get events --limit 20',
      },
    ],
    note:
      'routerd はプレリリースの v1alpha1 ソフトウェアです。最初の live 実行を、そのネットワークで唯一のルーターや唯一のリモート管理経路に対して行わないでください。',
  },
  'zh-Hant': {
    title: '以 YAML 宣告 Ubuntu Server 路由器設定',
    description:
      'routerd 將具型別的 YAML 資源套用到 Ubuntu Server 路由器。FreeBSD 與 NixOS 目前只有安裝與服務管理整合的基礎工作，原生平台轉譯器仍在開發中。',
    eyebrow: '請先在隔離的 Ubuntu Server VM 測試',
    headline: '先描述路由器，再決定何時變更網路。',
    subtitle:
      'routerd 將 WAN、LAN、DNS、NAT、路由和系統設定明確寫入一份 YAML。請從實驗主機開始，並保留主控台或獨立管理介面：live 執行可能改變連線能力。',
    tutorial: '在 Ubuntu Server 開始',
    resources: '瀏覽設定項目',
    resourceModel: '資源如何運作',
    stable: '最新穩定版',
    operatorLoopTitle: '接著進行第一次安全的變更',
    operatorLoopSubtitle:
      '請依這個順序進行。驗證與 dry-run 不會變更主機網路；在主控台執行的 live apply 與守護程式可能會。請使用主控台或獨立管理路徑，並只在守護程式啟動後使用 routerctl。',
    operatorSteps: [
      {number: 1, label: '學習基本概念', command: 'WAN / LAN / DHCP / DNS'},
      {number: 2, label: '準備 Ubuntu 實驗環境', command: '主控台 / 管理 NIC'},
      {number: 3, label: '撰寫 YAML', command: 'router.yaml'},
      {number: 4, label: '只驗證', command: 'routerd validate'},
      {number: 5, label: '隔離 dry-run', command: 'routerd apply --once --dry-run'},
      {number: 6, label: '在主控台 live 套用', command: 'routerd apply --once'},
      {number: 7, label: '啟動守護程式後檢查', command: 'routerd serve → routerctl'},
    ],
    scenariosTitle: '請依這個順序開始',
    scenarios: [
      {
        title: '先學六個網路名詞',
        problem:
          '第一次實驗只需要 WAN、LAN、IP 位址、閘道、DHCP 和 DNS。指南不假設你已有網路經驗，會用簡短方式說明。',
        link: '/docs/tutorials/network-basics',
        linkLabel: '學習六個基本名詞 →',
        resources: ['WAN', 'LAN', 'DHCP', 'DNS'],
      },
      {
        title: '準備隔離的 Ubuntu Server 實驗環境',
        problem:
          '使用備用 VM 或主機，並保留主控台或獨立管理 NIC。不要在正式環境或唯一的路由器上進行第一次 live 變更。',
        link: '/docs/tutorials/getting-started',
        linkLabel: '準備實驗環境 →',
        resources: ['Ubuntu Server', '主控台存取', '獨立管理 NIC'],
      },
      {
        title: '撰寫一項小型路由器工作',
        problem:
          '先從介面與單一 LAN 服務開始，再逐一加入 DHCP、DNS 和對外 IPv4 NAT。',
        link: '/docs/tutorials/first-router',
        linkLabel: '建立第一台路由器 →',
        resources: ['Interface', 'DHCPv4Server', 'NAT44Rule'],
      },
      {
        title: 'FreeBSD 與 NixOS：仍在基礎建設階段',
        problem:
          '已有安裝配置與服務管理整合的基礎工作，但原生平台轉譯器和功能對等性仍待完成。第一台路由器請使用 Ubuntu Server。',
        link: '/docs/platforms',
        linkLabel: '查看平台狀態 →',
        resources: ['Ubuntu Server 為主', 'FreeBSD 基礎工作', 'NixOS 基礎工作'],
      },
    ],
    observabilityTitle: '守護程式啟動後再檢查路由器',
    observabilitySubtitle:
      'routerctl 會與本機正在執行的 routerd 守護程式通訊。它用於檢查已執行的路由器，不用於上面的獨立驗證與 dry-run。',
    observabilityItems: [
      {
        title: 'routerctl get status',
        description: '在進行下一項變更前，檢查守護程式回報的資源狀態。',
      },
      {
        title: 'routerctl doctor',
        description: '路由器啟動後，透過守護程式執行針對性的健康診斷。',
      },
      {
        title: 'routerctl get events',
        description: '資源未進入預期狀態時，檢視最近的控制器事件。',
      },
      {
        title: '保留主機主控台',
        description: 'live 網路變更影響遠端存取時，主控台或獨立管理路徑就是復原途徑。',
      },
    ],
    quickstartTitle: 'Ubuntu Server：先檢查，再上線',
    quickstartBody:
      '請在備用的 Ubuntu Server VM 或主機安裝 routerd，將範例 YAML 放到下列路徑；進行 live 步驟前，保留主控台或獨立管理路徑。',
    installLinkLabel: '閱讀 Ubuntu Server 安裝指南',
    quickstartStages: [
      {
        title: '1. 只驗證設定檔',
        body: '此步驟會檢查 YAML 和資源規則，不會變更主機網路，也不需要 routerd 守護程式正在執行。',
        command: 'sudo routerd validate --config /usr/local/etc/routerd/router.yaml',
      },
      {
        title: '2. 執行隔離的 dry-run',
        body: '此步驟會使用暫存的 state、ledger 與 status 路徑執行一次套用流程，不會套用網路變更或寫入 routerd 的正常狀態檔。它仍是獨立操作，不需要守護程式或 routerctl。',
        command: 'LAB_DIR="$(mktemp -d)"\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager --state-file "$LAB_DIR/state.db" --ledger-file "$LAB_DIR/ledger.db" --status-file "$LAB_DIR/status.json"',
      },
      {
        title: '3. 從主控台進行 live 套用',
        body: '這會變更主機網路後結束。只在實驗環境的主控台，或有獨立管理路徑時執行。',
        command: '# 一次 live 套用：變更主機網路後結束。\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once',
      },
      {
        title: '4. 啟動守護程式，再使用 routerctl',
        body: 'routerd serve 是 live 執行，會持續套用並協調主機網路。啟動後才在另一個終端機使用 routerctl。',
        command: '# 從主控台啟動 live 守護程式。\nsudo routerd serve --config /usr/local/etc/routerd/router.yaml\n\n# routerd serve 啟動後，於另一個終端機執行。\nsudo routerctl get status\nsudo routerctl get events --limit 20',
      },
    ],
    note:
      'routerd 是 pre-release v1alpha1 軟體。不要將第一次 live 執行用在網路中唯一的路由器或唯一的遠端管理路徑。',
  },
  'zh-Hans': {
    title: '用 YAML 声明 Ubuntu Server 路由器配置',
    description:
      'routerd 将带类型的 YAML 资源应用到 Ubuntu Server 路由器。FreeBSD 和 NixOS 目前只有安装与服务管理集成的基础工作，原生平台渲染器仍在开发中。',
    eyebrow: '请先在隔离的 Ubuntu Server VM 中测试',
    headline: '先描述路由器，再决定何时变更网络。',
    subtitle:
      'routerd 将 WAN、LAN、DNS、NAT、路由和系统设置明确写入一份 YAML。请从实验主机开始，并保留控制台或独立管理网卡：live 运行可能改变连通性。',
    tutorial: '在 Ubuntu Server 上开始',
    resources: '浏览配置项',
    resourceModel: '资源如何工作',
    stable: '最新稳定版',
    operatorLoopTitle: '接着进行第一次安全的变更',
    operatorLoopSubtitle:
      '请按这个顺序进行。验证和 dry-run 不会变更主机网络；在控制台运行的 live apply 和守护进程可能会。请使用控制台或独立管理路径，并且只在守护进程启动后使用 routerctl。',
    operatorSteps: [
      {number: 1, label: '学习基本概念', command: 'WAN / LAN / DHCP / DNS'},
      {number: 2, label: '准备 Ubuntu 实验环境', command: '控制台 / 管理 NIC'},
      {number: 3, label: '编写 YAML', command: 'router.yaml'},
      {number: 4, label: '仅验证', command: 'routerd validate'},
      {number: 5, label: '隔离 dry-run', command: 'routerd apply --once --dry-run'},
      {number: 6, label: '在控制台 live 应用', command: 'routerd apply --once'},
      {number: 7, label: '启动守护进程后检查', command: 'routerd serve → routerctl'},
    ],
    scenariosTitle: '请按这个顺序开始',
    scenarios: [
      {
        title: '先学六个网络名词',
        problem:
          '第一次实验只需要 WAN、LAN、IP 地址、网关、DHCP 和 DNS。指南不假设你已有网络经验，会用简短方式说明。',
        link: '/docs/tutorials/network-basics',
        linkLabel: '学习六个基本名词 →',
        resources: ['WAN', 'LAN', 'DHCP', 'DNS'],
      },
      {
        title: '准备隔离的 Ubuntu Server 实验环境',
        problem:
          '使用备用 VM 或主机，并保留控制台或独立管理 NIC。不要在生产环境或唯一的路由器上进行第一次 live 变更。',
        link: '/docs/tutorials/getting-started',
        linkLabel: '准备实验环境 →',
        resources: ['Ubuntu Server', '控制台访问', '独立管理 NIC'],
      },
      {
        title: '编写一项小型路由器任务',
        problem:
          '先从接口和单一 LAN 服务开始，再逐一加入 DHCP、DNS 和出站 IPv4 NAT。',
        link: '/docs/tutorials/first-router',
        linkLabel: '构建第一台路由器 →',
        resources: ['Interface', 'DHCPv4Server', 'NAT44Rule'],
      },
      {
        title: 'FreeBSD 和 NixOS：仍处于基础建设阶段',
        problem:
          '已有安装布局和服务管理集成的基础工作，但原生平台渲染器和功能对等性仍待完成。第一台路由器请使用 Ubuntu Server。',
        link: '/docs/platforms',
        linkLabel: '查看平台状态 →',
        resources: ['Ubuntu Server 为主', 'FreeBSD 基础工作', 'NixOS 基础工作'],
      },
    ],
    observabilityTitle: '守护进程启动后再检查路由器',
    observabilitySubtitle:
      'routerctl 会与本地正在运行的 routerd 守护进程通信。它用于检查已运行的路由器，不用于上面的独立验证和 dry-run。',
    observabilityItems: [
      {
        title: 'routerctl get status',
        description: '在进行下一项变更前，检查守护进程报告的资源状态。',
      },
      {
        title: 'routerctl doctor',
        description: '路由器启动后，通过守护进程执行有针对性的健康诊断。',
      },
      {
        title: 'routerctl get events',
        description: '资源未进入预期状态时，查看最近的控制器事件。',
      },
      {
        title: '保留主机控制台',
        description: 'live 网络变更影响远程访问时，控制台或独立管理路径就是恢复途径。',
      },
    ],
    quickstartTitle: 'Ubuntu Server：先检查，再上线',
    quickstartBody:
      '请在备用的 Ubuntu Server VM 或主机上安装 routerd，将示例 YAML 放到下列路径；进行 live 步骤前，保留控制台或独立管理路径。',
    installLinkLabel: '阅读 Ubuntu Server 安装指南',
    quickstartStages: [
      {
        title: '1. 只验证配置文件',
        body: '这会检查 YAML 和资源规则，不会变更主机网络，也不需要 routerd 守护进程正在运行。',
        command: 'sudo routerd validate --config /usr/local/etc/routerd/router.yaml',
      },
      {
        title: '2. 运行隔离的 dry-run',
        body: '这会使用临时的 state、ledger 和 status 路径执行一次应用流程，不应用网络变更，也不写入 routerd 的正常状态文件。它仍是独立操作，不需要守护进程或 routerctl。',
        command: 'LAB_DIR="$(mktemp -d)"\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager --state-file "$LAB_DIR/state.db" --ledger-file "$LAB_DIR/ledger.db" --status-file "$LAB_DIR/status.json"',
      },
      {
        title: '3. 从控制台进行 live 应用',
        body: '这会变更主机网络后退出。只在实验环境的控制台，或有独立管理路径时运行。',
        command: '# 一次 live 应用：变更主机网络后退出。\nsudo routerd apply --config /usr/local/etc/routerd/router.yaml --once',
      },
      {
        title: '4. 启动守护进程，再使用 routerctl',
        body: 'routerd serve 是 live 运行，会持续应用并协调主机网络。启动后才在另一个终端中使用 routerctl。',
        command: '# 从控制台启动 live 守护进程。\nsudo routerd serve --config /usr/local/etc/routerd/router.yaml\n\n# routerd serve 启动后，在另一个终端中运行。\nsudo routerctl get status\nsudo routerctl get events --limit 20',
      },
    ],
    note:
      'routerd 是 pre-release v1alpha1 软件。不要把第一次 live 运行用于网络中唯一的路由器或唯一的远程管理路径。',
  },
};

function HomepageHeader({siteCopy}: {siteCopy: SiteCopy}) {
  const {i18n} = useDocusaurusContext();
  return (
    <header className="heroBanner">
      <div className="container heroInner">
        <div className="heroEyebrow">{siteCopy.eyebrow}</div>
        <Heading as="h1" className="heroTitle">
          routerd
        </Heading>
        <p className={styles.heroHeadline}>{siteCopy.headline}</p>
        <p className="heroSubtitle">{siteCopy.subtitle}</p>
        <p className={styles.heroStable}>
          <Link to="/docs/releases/stable">
            {siteCopy.stable}: <b>{STABLE_VERSION}</b>
          </Link>
        </p>
        <div className="heroActions">
          <Link className="button button--secondary button--lg" to="/docs/tutorials/getting-started">
            {siteCopy.tutorial}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/reference/api-v1alpha1">
            {siteCopy.resources}
          </Link>
          {i18n.currentLocale === 'en' && (
            <Link className="button button--outline button--secondary button--lg" to="/wizard">
              Configuration wizard
            </Link>
          )}
          <Link className="button button--outline button--secondary button--lg" to="/docs/concepts/resource-model">
            {siteCopy.resourceModel}
          </Link>
        </div>
      </div>
    </header>
  );
}

function OperatorLoop({siteCopy}: {siteCopy: SiteCopy}) {
  return (
    <section className={styles.operatorLoop}>
      <div className="container">
        <Heading as="h2">{siteCopy.operatorLoopTitle}</Heading>
        <p className={styles.operatorLoopSubtitle}>{siteCopy.operatorLoopSubtitle}</p>
        <div className={styles.operatorPipeline}>
          {siteCopy.operatorSteps.map((step, idx) => (
            <div className={styles.operatorStep} key={step.number}>
              <div className={styles.operatorStepNumber}>{step.number}</div>
              <div className={styles.operatorStepLabel}>{step.label}</div>
              <code className={styles.operatorStepCommand}>{step.command}</code>
              {idx < siteCopy.operatorSteps.length - 1 && (
                <span className={styles.operatorArrow} aria-hidden="true">{'→'}</span>
              )}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function ScenarioCards({siteCopy}: {siteCopy: SiteCopy}) {
  return (
    <section className={styles.scenarios}>
      <div className="container">
        <Heading as="h2">{siteCopy.scenariosTitle}</Heading>
        <div className={styles.scenarioGrid}>
          {siteCopy.scenarios.map((card) => (
            <article className={styles.scenarioCard} key={card.title}>
              <Heading as="h3" className={styles.scenarioCardTitle}>{card.title}</Heading>
              <p className={styles.scenarioCardProblem}>{card.problem}</p>
              <ul className={styles.scenarioResources}>
                {card.resources.map((resource) => (
                  <li key={resource}><code>{resource}</code></li>
                ))}
              </ul>
              <Link to={card.link} className={styles.scenarioLink}>
                {card.linkLabel}
              </Link>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function Observability({siteCopy}: {siteCopy: SiteCopy}) {
  return (
    <section className={styles.observability}>
      <div className="container">
        <Heading as="h2">{siteCopy.observabilityTitle}</Heading>
        <p className={styles.observabilitySubtitle}>{siteCopy.observabilitySubtitle}</p>
        <div className={styles.observabilityGrid}>
          {siteCopy.observabilityItems.map((item) => (
            <article className={styles.observabilityItem} key={item.title}>
              <Heading as="h3" className={styles.observabilityItemTitle}>{item.title}</Heading>
              <p>{item.description}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function Quickstart({siteCopy}: {siteCopy: SiteCopy}) {
  return (
    <section className={clsx('section', styles.quickstart)}>
      <div className="container">
        <Heading as="h2">{siteCopy.quickstartTitle}</Heading>
        <p>{siteCopy.quickstartBody}</p>
        <p>
          <Link to="/docs/install-and-upgrade">{siteCopy.installLinkLabel} →</Link>
        </p>
        {siteCopy.quickstartStages.map((stage) => (
          <div key={stage.title}>
            <Heading as="h3">{stage.title}</Heading>
            <p>{stage.body}</p>
            <pre className="terminal"><code>{stage.command}</code></pre>
          </div>
        ))}
        <p className={styles.note}>{siteCopy.note}</p>
      </div>
    </section>
  );
}

export default function Home(): JSX.Element {
  const {i18n} = useDocusaurusContext();
  const siteCopy = copy[i18n.currentLocale as SupportedHomepageLocale] ?? copy.en;
  return (
    <Layout title={siteCopy.title} description={siteCopy.description}>
      <HomepageHeader siteCopy={siteCopy} />
      <main>
        <ScenarioCards siteCopy={siteCopy} />
        <OperatorLoop siteCopy={siteCopy} />
        <Quickstart siteCopy={siteCopy} />
        <Observability siteCopy={siteCopy} />
      </main>
    </Layout>
  );
}
