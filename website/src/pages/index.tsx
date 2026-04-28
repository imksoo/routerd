import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

const copy = {
  en: {
    title: 'Declarative router control',
    description: 'routerd is a declarative router resource applier for Linux hosts',
    eyebrow: 'Open router control plane',
    subtitle:
      'A declarative router resource applier for small networks that need explicit configuration, repeatable installs, and practical observability.',
    tutorial: 'Start the tutorial',
    resources: 'Browse resources',
    quickstartTitle: 'From YAML To Router State',
    quickstartBody:
      'Validate a config, inspect the plan, and apply it in one-shot mode before enabling the daemon.',
    features: [
      {
        title: 'Declarative Router Resources',
        body: 'Describe interfaces, DHCP, DNS, NAT, DS-Lite, route policy, sysctl, and local system behavior as typed YAML resources.',
      },
      {
        title: 'Built For Real Hosts',
        body: 'routerd applies Linux networking components such as systemd-networkd, dnsmasq, nftables, pppd, and systemd services.',
      },
      {
        title: 'Small, Inspectable Control Plane',
        body: 'A Go daemon, one-shot CLI mode, JSON status, and an HTTP+JSON v1alpha1 control API keep operations understandable.',
      },
    ],
  },
  ja: {
    title: '宣言的ルーター制御',
    description: 'routerd は Linux ホスト向けの宣言的ルーターリソース収束ツールです',
    eyebrow: 'オープンなルーター制御プレーン',
    subtitle:
      '小規模ネットワークを、明示的な設定、再現しやすいインストール、実用的な観測性で運用するための宣言的ルーターリソース収束ツールです。',
    tutorial: 'チュートリアルを始める',
    resources: 'リソースを見る',
    quickstartTitle: 'YAML からルーター状態へ',
    quickstartBody:
      'デーモンを有効にする前に設定を検証し、計画を確認して、一度だけ望ましい状態へ収束させます。',
    features: [
      {
        title: '宣言的なルーターリソース',
        body: 'インターフェース、DHCP、DNS、NAT、DS-Lite、経路ポリシー、sysctl、ローカルシステムの振る舞いを型付き YAML リソースとして記述します。',
      },
      {
        title: '実ホスト向け',
        body: 'routerd は systemd-networkd、dnsmasq、nftables、pppd、systemd サービスなどの Linux ネットワーク部品を望ましい状態へ収束させます。',
      },
      {
        title: '小さく読める制御プレーン',
        body: 'Go 製デーモン、一度だけ実行できる CLI、JSON 状態出力、HTTP+JSON v1alpha1 制御 API で運用時の見通しを保ちます。',
      },
    ],
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
        <p className="heroSubtitle">
          {siteCopy.subtitle}
        </p>
        <div className="heroActions">
          <Link className="button button--secondary button--lg" to="/docs/tutorials/getting-started">
            {siteCopy.tutorial}
          </Link>
          <Link className="button button--outline button--secondary button--lg" to="/docs/reference/api-v1alpha1">
            {siteCopy.resources}
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
    <Layout
      title={siteCopy.title}
      description={siteCopy.description}>
      <HomepageHeader siteCopy={siteCopy} />
      <main>
        <section className="section">
          <div className="container">
            <div className="featureGrid">
              {siteCopy.features.map((feature) => (
                <article className="featureItem" key={feature.title}>
                  <Heading as="h2">{feature.title}</Heading>
                  <p>{feature.body}</p>
                </article>
              ))}
            </div>
          </div>
        </section>
        <section className={clsx('section', styles.quickstart)}>
          <div className="container">
            <Heading as="h2">{siteCopy.quickstartTitle}</Heading>
            <p>
              {siteCopy.quickstartBody}
            </p>
            <pre className="terminal"><code>{`routerd validate --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
routerd serve --config /usr/local/etc/routerd/router.yaml`}</code></pre>
          </div>
        </section>
      </main>
    </Layout>
  );
}
