// SPDX-License-Identifier: BSD-3-Clause

import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

const STABLE_VERSION = 'v20260608.2325';

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

const copy = {
  en: {
    title: 'Declarative router control for real hosts',
    description:
      'routerd turns typed YAML resources into a working, observable router on Linux, NixOS, and FreeBSD hosts.',
    eyebrow: 'Open router control plane',
    headline: 'Make a host router readable again.',
    subtitle:
      'routerd describes WAN acquisition, LAN services, DNS, NAT, route policy, system bootstrap, and observability as typed resources. It is built for small networks where the router must be explicit, repeatable, and inspectable.',
    tutorial: 'Install routerd',
    resources: 'Browse resources',
    configWizard: 'Config wizard',
    resourceModel: 'Resource model',
    stable: 'Latest stable',
    operatorLoopTitle: 'The Operator Loop',
    operatorLoopSubtitle:
      'Six steps from intent to observation. Each step is safe to repeat.',
    operatorSteps: [
      {number: 1, label: 'Declare', command: 'router.yaml'},
      {number: 2, label: 'Validate', command: 'routerctl validate'},
      {number: 3, label: 'Plan', command: 'routerctl plan'},
      {number: 4, label: 'Apply', command: 'routerctl apply'},
      {number: 5, label: 'Serve', command: 'routerd serve'},
      {number: 6, label: 'Observe', command: 'routerctl doctor / get / describe'},
    ] as OperatorStep[],
    scenariosTitle: 'What You Can Build',
    scenarios: [
      {
        title: 'Home DS-Lite / PPPoE / multi-WAN router',
        problem:
          'A single YAML declares WAN acquisition, prefix delegation, NAT, DNS, and failover for a residential edge.',
        link: '/docs/tutorials/getting-started',
        linkLabel: 'Start here →',
        resources: ['DHCPv6PD', 'DSLiteTunnel', 'EgressRoutePolicy'],
      },
      {
        title: 'Proxmox or lab edge router',
        problem:
          'Overlay tunnels and bridge adoption turn a hypervisor host into a declarative lab router.',
        link: '/docs/how-to/pve-overlay',
        linkLabel: 'Start here →',
        resources: ['Interface', 'WireGuardInterface', 'VXLAN'],
      },
      {
        title: 'NixOS / FreeBSD host router',
        problem:
          'Same resource model renders to systemd, networkd, nftables, rc.conf, or pf depending on the host OS.',
        link: '/docs/tutorials/nixos-getting-started',
        linkLabel: 'Start here →',
        resources: ['Package', 'SysctlProfile', 'cross-OS render'],
      },
      {
        title: 'Tailscale / WireGuard / overlay edge',
        problem:
          'Integrate site-to-site or exit-node overlays while keeping the local LAN declarative.',
        link: '/docs/how-to/tailscale',
        linkLabel: 'Start here →',
        resources: ['TailscaleNode', 'WireGuardInterface'],
      },
      {
        title: 'CloudEdge SAM selected /32 mobility',
        problem:
          'Selective address mobility lets cloud VMs capture and release /32 addresses across providers.',
        link: '/docs/reference/selective-address-mobility',
        linkLabel: 'Start here →',
        resources: ['CaptureAddress', 'EventGroup'],
      },
      {
        title: 'Observable router operations',
        problem:
          'Treat the router like a service: health checks, event streams, connection inspection, and telemetry.',
        link: '/docs/operations/routerctl-doctor',
        linkLabel: 'Start here →',
        resources: ['routerctl doctor', 'events', 'Web Console', 'OTel'],
      },
    ] as ScenarioCard[],
    observabilityTitle: 'Observed Like a Service',
    observabilitySubtitle:
      'routerd exposes runtime state through multiple interfaces. No guessing.',
    observabilityItems: [
      {
        title: 'routerctl status / events / doctor',
        description:
          'CLI inspection of resource phases, event history, and automated health diagnostics.',
      },
      {
        title: 'Connection / conntrack inspection',
        description:
          'Live conntrack table queries with per-flow source, destination, NAT mapping, and byte counters.',
      },
      {
        title: 'Read-only Web Console',
        description:
          'Browser dashboard showing status, connections, DNS queries, traffic, firewall logs, and config.',
      },
      {
        title: 'Logs / OpenTelemetry',
        description:
          'Structured log sinks, metrics, and traces exported to any OTel-compatible collector.',
      },
    ] as ObservabilityItem[],
    quickstartTitle: 'Install, Validate, Apply',
    quickstartBody:
      'Start from the release archive. The installer brings in runtime packages, installs binaries and service templates, then you validate the YAML before changing the host.',
    note:
      'routerd is pre-release v1alpha1 software. The project favors clear, safe router semantics over compatibility with early experimental names.',
  },
  ja: {
    title: '実ホスト向け宣言的ルーター制御',
    description:
      'routerd は型付き YAML リソースを、動作し観測できる Linux、NixOS、FreeBSD ルーターへ反映します。',
    eyebrow: 'オープンなルーター制御プレーン',
    headline: 'ホストルーターを、もう一度読める形に。',
    subtitle:
      'routerd は WAN 取得、LAN サービス、DNS、NAT、経路ポリシー、OS 準備、観測性を型付きリソースとして記述します。小規模ネットワークを明示的に、再現しやすく、確認しやすく運用するためのソフトウェアです。',
    tutorial: 'routerd を導入する',
    resources: 'リソースを見る',
    configWizard: 'Config ウィザード',
    resourceModel: 'リソースモデル',
    stable: '最新安定版',
    operatorLoopTitle: 'オペレーターループ',
    operatorLoopSubtitle:
      '意図から観測まで6ステップ。各ステップは安全に繰り返せます。',
    operatorSteps: [
      {number: 1, label: '宣言', command: 'router.yaml'},
      {number: 2, label: '検証', command: 'routerctl validate'},
      {number: 3, label: '計画', command: 'routerctl plan'},
      {number: 4, label: '適用', command: 'routerctl apply'},
      {number: 5, label: '稼働', command: 'routerd serve'},
      {number: 6, label: '観測', command: 'routerctl doctor / get / describe'},
    ] as OperatorStep[],
    scenariosTitle: '構成できるもの',
    scenarios: [
      {
        title: '家庭用 DS-Lite / PPPoE / マルチWAN ルーター',
        problem:
          '1つのYAMLでWAN取得、プレフィックス委任、NAT、DNS、フェイルオーバーを宣言的に構成します。',
        link: '/docs/tutorials/getting-started',
        linkLabel: 'ここから始める →',
        resources: ['DHCPv6PD', 'DSLiteTunnel', 'EgressRoutePolicy'],
      },
      {
        title: 'Proxmox / ラボ用エッジルーター',
        problem:
          'オーバーレイトンネルとブリッジ引き継ぎで、ハイパーバイザーホストを宣言的ラボルーターにします。',
        link: '/docs/how-to/pve-overlay',
        linkLabel: 'ここから始める →',
        resources: ['Interface', 'WireGuardInterface', 'VXLAN'],
      },
      {
        title: 'NixOS / FreeBSD ホストルーター',
        problem:
          '同じリソースモデルがホストOSに応じて systemd / networkd / nftables / rc.conf / pf へ反映されます。',
        link: '/docs/tutorials/nixos-getting-started',
        linkLabel: 'ここから始める →',
        resources: ['Package', 'SysctlProfile', 'クロスOS レンダー'],
      },
      {
        title: 'Tailscale / WireGuard / オーバーレイエッジ',
        problem:
          '拠点間接続やexit-nodeオーバーレイを統合しつつ、ローカルLANは宣言的に管理します。',
        link: '/docs/how-to/tailscale',
        linkLabel: 'ここから始める →',
        resources: ['TailscaleNode', 'WireGuardInterface'],
      },
      {
        title: 'CloudEdge SAM 選択的 /32 モビリティ',
        problem:
          'クラウドVMがプロバイダー間で /32 アドレスを自律的にキャプチャ・リリースします。',
        link: '/docs/reference/selective-address-mobility',
        linkLabel: 'ここから始める →',
        resources: ['CaptureAddress', 'EventGroup'],
      },
      {
        title: '観測可能なルーター運用',
        problem:
          'ルーターをサービスのように扱います：ヘルスチェック、イベントストリーム、接続確認、テレメトリー。',
        link: '/docs/operations/routerctl-doctor',
        linkLabel: 'ここから始める →',
        resources: ['routerctl doctor', 'events', 'Web Console', 'OTel'],
      },
    ] as ScenarioCard[],
    observabilityTitle: 'サービスのように観測する',
    observabilitySubtitle:
      'routerd は複数のインターフェースで実行時状態を公開します。推測は不要です。',
    observabilityItems: [
      {
        title: 'routerctl status / events / doctor',
        description:
          'リソースフェーズ、イベント履歴、自動ヘルス診断の CLI 確認。',
      },
      {
        title: 'コネクション / conntrack 確認',
        description:
          'フローごとの送信元、宛先、NAT マッピング、バイトカウンターを含む conntrack テーブルのライブクエリ。',
      },
      {
        title: '読み取り専用 Web Console',
        description:
          '状態、コネクション、DNS クエリ、通信量、ファイアウォールログ、設定を表示するブラウザダッシュボード。',
      },
      {
        title: 'ログ / OpenTelemetry',
        description:
          '構造化ログシンク、メトリクス、トレースを OTel 互換コレクターへエクスポート。',
      },
    ] as ObservabilityItem[],
    quickstartTitle: '導入、検証、適用',
    quickstartBody:
      'リリースアーカイブから始めます。インストーラーが実行時パッケージ、実行ファイル、サービステンプレートを配置します。その後、YAML を検証してからホストを変更します。',
    note:
      'routerd はプレリリースの v1alpha1 ソフトウェアです。初期実験名との互換性より、分かりやすく安全なルーターの意味を優先します。',
  },
};

function HomepageHeader({siteCopy}: {siteCopy: typeof copy.en}) {
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
          <Link className="button button--secondary button--lg" to="/docs/install-and-upgrade">
            {siteCopy.tutorial}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/reference/api-v1alpha1">
            {siteCopy.resources}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/wizard">
            {siteCopy.configWizard}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/concepts/resource-model">
            {siteCopy.resourceModel}
          </Link>
        </div>
      </div>
    </header>
  );
}

function OperatorLoop({siteCopy}: {siteCopy: typeof copy.en}) {
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

function ScenarioCards({siteCopy}: {siteCopy: typeof copy.en}) {
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
                {card.resources.map((r) => (
                  <li key={r}><code>{r}</code></li>
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

function Observability({siteCopy}: {siteCopy: typeof copy.en}) {
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

function Quickstart({siteCopy}: {siteCopy: typeof copy.en}) {
  return (
    <section className={clsx('section', styles.quickstart)}>
      <div className="container">
        <Heading as="h2">{siteCopy.quickstartTitle}</Heading>
        <p>{siteCopy.quickstartBody}</p>
        <pre className="terminal"><code>{`curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
routerd serve --config /usr/local/etc/routerd/router.yaml`}</code></pre>
        <p className={styles.note}>{siteCopy.note}</p>
      </div>
    </section>
  );
}

export default function Home(): JSX.Element {
  const {i18n} = useDocusaurusContext();
  const siteCopy = i18n.currentLocale === 'ja' ? copy.ja : copy.en;
  return (
    <Layout title={siteCopy.title} description={siteCopy.description}>
      <HomepageHeader siteCopy={siteCopy} />
      <main>
        <OperatorLoop siteCopy={siteCopy} />
        <ScenarioCards siteCopy={siteCopy} />
        <Observability siteCopy={siteCopy} />
        <Quickstart siteCopy={siteCopy} />
      </main>
    </Layout>
  );
}
