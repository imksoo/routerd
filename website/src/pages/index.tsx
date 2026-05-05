import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

const copy = {
  en: {
    title: 'Declarative router control for real hosts',
    description:
      'routerd turns typed YAML resources into a working, observable router on a Linux host.',
    eyebrow: 'Open router control plane',
    headline: 'Make a host router readable again.',
    subtitle:
      'routerd describes WAN acquisition, LAN services, DNS, NAT, route policy, system bootstrap, and observability as typed resources. It is built for small networks where the router must be explicit, repeatable, and inspectable.',
    tutorial: 'Start the tutorial',
    resources: 'Browse resources',
    config: 'See configuration examples',
    quickstartTitle: 'Validate, Plan, Apply',
    quickstartBody:
      'Start with a normal YAML file. Validate it, inspect the plan, run a dry application, then let the daemon keep the host converged.',
    pillars: [
      {
        title: 'One Router Intent',
        body: 'Interfaces, DHCP, RA, DNS zones, DoH/DoT/DoQ forwarding, DS-Lite, NAT44, route policy, sysctl, packages, and systemd units live in one resource model.',
      },
      {
        title: 'Managed Protocol Daemons',
        body: 'DHCPv4, DHCPv6-PD, PPPoE, DNS, health checks, event relay, and firewall logging expose local HTTP+JSON status instead of hiding state in hooks.',
      },
      {
        title: 'Operational By Default',
        body: 'routerctl, SQLite-backed events, log sinks, OpenTelemetry hooks, conntrack inspection, and a read-only Web Console keep runtime behavior visible.',
      },
    ],
    outcomesTitle: 'What It Can Build',
    outcomes: [
      'DHCPv6-PD and DS-Lite with AFTR conditional DNS resolution',
      'DHCPv4 scopes, reservations, DHCPv6, RA, RDNSS, DNSSL, and MTU options',
      'Local DNS zones, DHCP-derived records, private upstreams, cache, and DNSSEC flags',
      'Egress route selection with health checks, NAT44 exclusions, and conntrack preservation',
      'Declarative packages, sysctl profiles, network adoption, systemd units, and log forwarding',
      'Read-only Web Console for status, events, connections, DNS queries, traffic, firewall logs, and config',
    ],
    note:
      'routerd is pre-release v1alpha1 software. The project favors clear, safe router semantics over compatibility with early experimental names.',
  },
  ja: {
    title: '実ホスト向け宣言的ルーター制御',
    description:
      'routerd は型付き YAML リソースを、動作し観測できる Linux ルーターへ反映します。',
    eyebrow: 'オープンなルーター制御プレーン',
    headline: 'ホストルーターを、もう一度読める形に。',
    subtitle:
      'routerd は WAN 取得、LAN サービス、DNS、NAT、経路ポリシー、OS 準備、観測性を型付きリソースとして記述します。小規模ネットワークを明示的に、再現しやすく、確認しやすく運用するためのソフトウェアです。',
    tutorial: 'チュートリアルを始める',
    resources: 'リソースを見る',
    config: '設定例を見る',
    quickstartTitle: '検証、計画、適用',
    quickstartBody:
      '普通の YAML ファイルから始めます。検証し、計画を確認し、予行実行してから、デーモンでホストを望ましい状態に保ちます。',
    pillars: [
      {
        title: '1 つのルーター意図',
        body: 'インターフェース、DHCP、RA、DNS ゾーン、DoH/DoT/DoQ 転送、DS-Lite、NAT44、経路ポリシー、sysctl、パッケージ、systemd ユニットを同じリソースモデルで扱います。',
      },
      {
        title: '管理対象プロトコルデーモン',
        body: 'DHCPv4、DHCPv6-PD、PPPoE、DNS、ヘルスチェック、イベント中継、ファイアウォールログは、hook に状態を隠さずローカル HTTP+JSON で公開します。',
      },
      {
        title: '運用時に見える',
        body: 'routerctl、SQLite イベント、ログ転送、OpenTelemetry、conntrack 確認、読み取り専用 Web Console で実行中の振る舞いを確認できます。',
      },
    ],
    outcomesTitle: '構成できるもの',
    outcomes: [
      'DHCPv6-PD と、AFTR 条件付き DNS 解決を含む DS-Lite',
      'DHCPv4 スコープ、固定割り当て、DHCPv6、RA、RDNSS、DNSSL、MTU オプション',
      'ローカル DNS ゾーン、DHCP 由来レコード、専用上流、キャッシュ、DNSSEC フラグ',
      'ヘルスチェック付き経路選択、NAT44 対象外指定、conntrack を消さない経路変更',
      'パッケージ、sysctl プロファイル、ネットワーク引き継ぎ、systemd ユニット、ログ転送',
      '状態、イベント、コネクション、DNS クエリー、通信、ファイアウォールログ、設定を表示する Web Console',
    ],
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
        <div className="heroActions">
          <Link className="button button--secondary button--lg" to="/docs/tutorials/getting-started">
            {siteCopy.tutorial}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/reference/api-v1alpha1">
            {siteCopy.resources}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/tutorials/router-lab">
            {siteCopy.config}
          </Link>
        </div>
      </div>
    </header>
  );
}

export default function Home(): JSX.Element {
  const {i18n} = useDocusaurusContext();
  const siteCopy = i18n.currentLocale === 'ja' ? copy.ja : copy.en;
  return (
    <Layout title={siteCopy.title} description={siteCopy.description}>
      <HomepageHeader siteCopy={siteCopy} />
      <main>
        <section className="section">
          <div className="container">
            <div className="featureGrid">
              {siteCopy.pillars.map((feature) => (
                <article className="featureItem" key={feature.title}>
                  <Heading as="h2">{feature.title}</Heading>
                  <p>{feature.body}</p>
                </article>
              ))}
            </div>
          </div>
        </section>
        <section className={styles.outcomes}>
          <div className="container">
            <Heading as="h2">{siteCopy.outcomesTitle}</Heading>
            <div className={styles.outcomeGrid}>
              {siteCopy.outcomes.map((item) => (
                <div className={styles.outcomeItem} key={item}>
                  {item}
                </div>
              ))}
            </div>
          </div>
        </section>
        <section className={clsx('section', styles.quickstart)}>
          <div className="container">
            <Heading as="h2">{siteCopy.quickstartTitle}</Heading>
            <p>{siteCopy.quickstartBody}</p>
            <pre className="terminal"><code>{`routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
routerd serve --config /usr/local/etc/routerd/router.yaml`}</code></pre>
            <p className={styles.note}>{siteCopy.note}</p>
          </div>
        </section>
      </main>
    </Layout>
  );
}
