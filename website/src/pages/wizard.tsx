// SPDX-License-Identifier: BSD-3-Clause

import React, {useMemo, useState} from 'react';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import {
  DEFAULT_HOME_ROUTER_STATE,
  buildK8SRouterYaml,
  buildSAMRouterYamls,
  buildHomeRouterYaml,
  mergeK8SWizardState,
  mergeHomeRouterState,
  mergeSAMWizardState,
  type HomeRouterWizardState,
  type HomeWanMode,
  type K8SBGPConvergenceProfile,
  type K8SBGPSessionType,
  type K8SBGPTimerProfile,
  type K8SWizardState,
  type SAMNode,
  type SAMNodeRole,
  type SAMProvider,
  type SAMWizardState,
  type WizardProfile,
} from '../lib/routerdWizard';
import styles from './wizard.module.css';

const homeSteps = ['Interfaces', 'WAN', 'LAN', 'HA', 'Output'] as const;
const samSteps = ['Nodes', 'Mobility', 'Output'] as const;
const k8sSteps = ['BGP', 'Routes', 'Output'] as const;
type Step = (typeof homeSteps)[number] | (typeof samSteps)[number] | (typeof k8sSteps)[number];

const wanModes: Array<{value: HomeWanMode; label: string}> = [
  {value: 'dhcpv4', label: 'DHCPv4 client'},
  {value: 'pppoe', label: 'PPPoE'},
  {value: 'dslite', label: 'DS-Lite'},
  {value: 'static', label: 'Static IPv4'},
];

const providers: Array<{value: SAMProvider; label: string}> = [
  {value: 'aws', label: 'AWS'},
  {value: 'azure', label: 'Azure'},
  {value: 'oci', label: 'OCI'},
];

export default function WizardPage(): JSX.Element {
  const [step, setStep] = useState<Step>('Interfaces');
  const [profile, setProfile] = useState<WizardProfile>('home');
  const [state, setState] = useState<HomeRouterWizardState>(() => mergeHomeRouterState());
  const [samState, setSAMState] = useState<SAMWizardState>(() => mergeSAMWizardState());
  const [k8sState, setK8SState] = useState<K8SWizardState>(() => mergeK8SWizardState());
  const [selectedOutput, setSelectedOutput] = useState('');
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const outputs = useMemo(() => {
    if (profile === 'sam') {
      return buildSAMRouterYamls(samState);
    }
    if (profile === 'k8s') {
      const name = `${k8sState.routerName || 'k8s-edge'}.yaml`;
      return {[name]: buildK8SRouterYaml(k8sState)};
    }
    const name = `${state.routerName || DEFAULT_HOME_ROUTER_STATE.routerName}.yaml`;
    return {[name]: buildHomeRouterYaml(state)};
  }, [k8sState, profile, samState, state]);
  const outputNames = useMemo(() => Object.keys(outputs).sort((a, b) => a.localeCompare(b)), [outputs]);
  const activeOutputName = outputs[selectedOutput] ? selectedOutput : outputNames[0] ?? 'router.yaml';
  const yaml = outputs[activeOutputName] ?? '';
  const activeSteps = profile === 'sam' ? samSteps : profile === 'k8s' ? k8sSteps : homeSteps;
  const stepIndex = Math.max((activeSteps as readonly Step[]).indexOf(step), 0);
  const resourceCount = useMemo(() => {
    const matches = yaml.match(/^\s{4}- apiVersion:/gm);
    return matches?.length ?? 0;
  }, [yaml]);

  function update(next: PartialHomeRouterWizardState): void {
    setState((current) => mergeHomeRouterState({
      routerName: next.routerName ?? current.routerName,
      interfaces: {...current.interfaces, ...(next.interfaces ?? {})},
      wan: {...current.wan, ...(next.wan ?? {})},
      lan: {...current.lan, ...(next.lan ?? {})},
      ha: {...current.ha, ...(next.ha ?? {})},
    }));
    setCopyState('idle');
  }

  function updateSAM(next: PartialSAMWizardState): void {
    setSAMState((current) => mergeSAMWizardState({
      name: next.name ?? current.name,
      mobilityPrefix: next.mobilityPrefix ?? current.mobilityPrefix,
      innerCIDR: next.innerCIDR ?? current.innerCIDR,
      bgpASN: next.bgpASN ?? current.bgpASN,
      routeReflectorNodeRef: next.routeReflectorNodeRef ?? current.routeReflectorNodeRef,
      nodes: next.nodes ?? current.nodes,
    }));
    setCopyState('idle');
  }

  function updateSAMNode(index: number, next: Partial<SAMNode>): void {
    updateSAM({
      nodes: samState.nodes.map((node, itemIndex) => itemIndex === index ? {...node, ...next} : node),
    });
  }

  function updateK8S(next: PartialK8SWizardState): void {
    setK8SState((current) => mergeK8SWizardState({
      routerName: next.routerName ?? current.routerName,
      bgpRouterName: next.bgpRouterName ?? current.bgpRouterName,
      bgpPeerName: next.bgpPeerName ?? current.bgpPeerName,
      sessionType: next.sessionType ?? current.sessionType,
      localASN: next.localASN ?? current.localASN,
      peerASN: next.peerASN ?? current.peerASN,
      routerID: next.routerID ?? current.routerID,
      listenAddress: next.listenAddress ?? current.listenAddress,
      peerAddresses: next.peerAddresses ?? current.peerAddresses,
      importPrefixes: next.importPrefixes ?? current.importPrefixes,
      exportPrefixes: next.exportPrefixes ?? current.exportPrefixes,
      redistributeConnected: next.redistributeConnected ?? current.redistributeConnected,
      redistributeStatic: next.redistributeStatic ?? current.redistributeStatic,
      ebgpMultihop: next.ebgpMultihop ?? current.ebgpMultihop,
      timersProfile: next.timersProfile ?? current.timersProfile,
      convergenceProfile: next.convergenceProfile ?? current.convergenceProfile,
    }));
    setCopyState('idle');
  }

  function selectProfile(next: WizardProfile): void {
    setProfile(next);
    setStep(next === 'sam' ? 'Nodes' : next === 'k8s' ? 'BGP' : 'Interfaces');
    setSelectedOutput('');
    setCopyState('idle');
  }

  async function copyYaml(): Promise<void> {
    try {
      await navigator.clipboard.writeText(yaml);
      setCopyState('copied');
    } catch {
      setCopyState('failed');
    }
  }

  function downloadYaml(): void {
    const blob = new Blob([yaml], {type: 'application/yaml'});
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = activeOutputName;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Layout title="Config wizard" description="Generate a routerd home router configuration.">
      <main className={styles.wizardPage}>
        <div className={`container ${styles.shell}`}>
          <aside className={styles.sidebar}>
            <nav className={styles.stepList} aria-label="Wizard steps">
              {activeSteps.map((item, index) => (
                <button
                  className={`${styles.stepButton} ${item === step ? styles.stepButtonActive : ''}`}
                  key={item}
                  onClick={() => setStep(item)}
                  type="button">
                  <span className={styles.stepNumber}>{index + 1}</span>
                  <span className={styles.stepText}>{item}</span>
                </button>
              ))}
            </nav>
            <div className={styles.summary}>
              {profile === 'home' && (
                <>
                  <div><b>{state.wan.mode}</b> WAN</div>
                  <div>{state.interfaces.lans.length} LAN interface{state.interfaces.lans.length === 1 ? '' : 's'}</div>
                </>
              )}
              {profile === 'sam' && (
                <>
                  <div><b>{samState.nodes.length}</b> SAM nodes</div>
                  <div>{samState.innerCIDR} inner CIDR</div>
                </>
              )}
              {profile === 'k8s' && (
                <>
                  <div><b>{k8sState.sessionType}</b> peering</div>
                  <div>{k8sState.peerAddresses.length} peer address{k8sState.peerAddresses.length === 1 ? '' : 'es'}</div>
                </>
              )}
              <div>{resourceCount} generated resources</div>
            </div>
          </aside>

          <section className={styles.panel}>
            <div className={styles.header}>
              <Heading as="h1">routerd config wizard</Heading>
              <p>Build routerd YAML from the same typed builder that generates the CI fixtures.</p>
            </div>

            <ProfileSelector profile={profile} setProfile={selectProfile} />

            {profile === 'home' && step === 'Interfaces' && <InterfacesStep state={state} update={update} />}
            {profile === 'home' && step === 'WAN' && <WanStep state={state} update={update} />}
            {profile === 'home' && step === 'LAN' && <LanStep state={state} update={update} />}
            {profile === 'home' && step === 'HA' && <HaStep state={state} update={update} />}
            {profile === 'sam' && step === 'Nodes' && (
              <SAMNodesStep
                addNode={() => updateSAM({nodes: [...samState.nodes, newSAMNode(samState.nodes.length + 1)]})}
                removeNode={(index) => updateSAM({nodes: samState.nodes.filter((_, itemIndex) => itemIndex !== index)})}
                state={samState}
                update={updateSAM}
                updateNode={updateSAMNode}
              />
            )}
            {profile === 'sam' && step === 'Mobility' && (
              <SAMMobilityStep state={samState} update={updateSAM} updateNode={updateSAMNode} />
            )}
            {profile === 'k8s' && step === 'BGP' && <K8SBGPStep state={k8sState} update={updateK8S} />}
            {profile === 'k8s' && step === 'Routes' && <K8SRoutesStep state={k8sState} update={updateK8S} />}
            {step === 'Output' && (
              <OutputStep
                activeOutputName={activeOutputName}
                copyState={copyState}
                downloadYaml={downloadYaml}
                outputNames={outputNames}
                resourceCount={resourceCount}
                copyYaml={copyYaml}
                setSelectedOutput={setSelectedOutput}
                summary={profile === 'sam'
                  ? [
                    ['Profile', 'Cloud Edge SAM'],
                    ['Nodes', String(samState.nodes.length)],
                    ['Inner CIDR', samState.innerCIDR],
                  ]
                  : profile === 'k8s'
                    ? [
                      ['Profile', 'Kubernetes BGP'],
                      ['Session', k8sState.sessionType],
                      ['Peers', String(k8sState.peerAddresses.length)],
                    ]
                  : [
                    ['Profile', 'Home Router'],
                    ['Router', state.routerName],
                    ['WAN mode', state.wan.mode],
                  ]}
                yaml={yaml}
              />
            )}

            <div className={styles.actions}>
              <button
                className={`${styles.button} ${styles.buttonSecondary}`}
                disabled={stepIndex === 0}
                onClick={() => setStep(activeSteps[Math.max(stepIndex - 1, 0)])}
                type="button">
                Back
              </button>
              <div className={styles.actionGroup}>
                {step !== 'Output' && (
                  <button
                    className={`${styles.button} ${styles.buttonPrimary}`}
                    onClick={() => setStep(activeSteps[Math.min(stepIndex + 1, activeSteps.length - 1)])}
                    type="button">
                    Next
                  </button>
                )}
                {step === 'Output' && (
                  <>
                    <button className={`${styles.button} ${styles.buttonSecondary}`} onClick={copyYaml} type="button">
                      Copy YAML
                    </button>
                    <button className={`${styles.button} ${styles.buttonPrimary}`} onClick={downloadYaml} type="button">
                      Download
                    </button>
                  </>
                )}
              </div>
            </div>
          </section>
        </div>
      </main>
    </Layout>
  );
}

function ProfileSelector({
  profile,
  setProfile,
}: {
  profile: WizardProfile;
  setProfile: (profile: WizardProfile) => void;
}): JSX.Element {
  return (
    <div className={styles.profileSelector} role="tablist" aria-label="Wizard profile">
      <button
        className={`${styles.profileButton} ${profile === 'home' ? styles.profileButtonActive : ''}`}
        onClick={() => setProfile('home')}
        type="button">
        Home Router
      </button>
      <button
        className={`${styles.profileButton} ${profile === 'sam' ? styles.profileButtonActive : ''}`}
        onClick={() => setProfile('sam')}
        type="button">
        Cloud Edge SAM
      </button>
      <button
        className={`${styles.profileButton} ${profile === 'k8s' ? styles.profileButtonActive : ''}`}
        onClick={() => setProfile('k8s')}
        type="button">
        Kubernetes
      </button>
    </div>
  );
}

function SAMNodesStep({
  addNode,
  removeNode,
  state,
  update,
  updateNode,
}: {
  addNode: () => void;
  removeNode: (index: number) => void;
  state: SAMWizardState;
  update: (next: PartialSAMWizardState) => void;
  updateNode: (index: number, next: Partial<SAMNode>) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>SAM nodes</Heading>
      <p className={styles.sectionLead}>Define the per-node bundle. Every generated node uses the same topology node set and inner CIDR.</p>
      <div className={styles.grid}>
        <TextField label="Bundle name" value={state.name} onChange={(name) => update({name})} />
        <NumberField label="BGP ASN" value={state.bgpASN} min={1} max={4294967295} onChange={(bgpASN) => update({bgpASN})} />
        <label className={styles.field}>
          <span className={styles.label}>Route reflector node</span>
          <select className={styles.select} value={state.routeReflectorNodeRef} onChange={(event) => update({routeReflectorNodeRef: event.target.value})}>
            {state.nodes.map((node) => (
              <option key={node.nodeRef} value={node.nodeRef}>{node.nodeRef}</option>
            ))}
          </select>
        </label>
      </div>
      <div className={styles.tableWrap}>
        <table className={styles.nodeTable}>
          <thead>
            <tr>
              <th>Node</th>
              <th>Role</th>
              <th>Site</th>
              <th>Underlay IPv4</th>
              <th>WireGuard endpoint</th>
              <th>Router ID</th>
              <th>Provider</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {state.nodes.map((node, index) => (
              <tr key={`${node.nodeRef}-${index}`}>
                <td><input className={styles.input} value={node.nodeRef} onChange={(event) => updateNode(index, {nodeRef: event.target.value})} /></td>
                <td>
                  <select
                    className={styles.select}
                    value={node.role}
                    onChange={(event) => {
                      const role = event.target.value as SAMNodeRole;
                      updateNode(index, role === 'cloud'
                        ? {role, provider: node.provider ?? 'aws', providerRef: node.providerRef || 'aws-lab'}
                        : {role, provider: undefined, providerRef: undefined});
                    }}>
                    <option value="onprem">on-prem</option>
                    <option value="cloud">cloud</option>
                  </select>
                </td>
                <td><input className={styles.input} value={node.site} onChange={(event) => updateNode(index, {site: event.target.value})} /></td>
                <td><input className={styles.input} value={node.underlayIPv4} onChange={(event) => updateNode(index, {underlayIPv4: event.target.value, routerID: event.target.value})} /></td>
                <td><input className={styles.input} value={node.wgEndpoint} onChange={(event) => updateNode(index, {wgEndpoint: event.target.value})} /></td>
                <td><input className={styles.input} value={node.routerID} onChange={(event) => updateNode(index, {routerID: event.target.value})} /></td>
                <td>
                  {node.role === 'cloud' ? (
                    <select className={styles.select} value={node.provider ?? 'aws'} onChange={(event) => updateNode(index, {provider: event.target.value as SAMProvider})}>
                      {providers.map((provider) => (
                        <option key={provider.value} value={provider.value}>{provider.label}</option>
                      ))}
                    </select>
                  ) : (
                    <span className={styles.muted}>proxy-ARP</span>
                  )}
                </td>
                <td>
                  <button className={`${styles.button} ${styles.buttonSecondary}`} disabled={state.nodes.length <= 1} onClick={() => removeNode(index)} type="button">Remove</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <button className={`${styles.button} ${styles.buttonSecondary}`} onClick={addNode} type="button">Add node</button>
    </>
  );
}

function SAMMobilityStep({
  state,
  update,
  updateNode,
}: {
  state: SAMWizardState;
  update: (next: PartialSAMWizardState) => void;
  updateNode: (index: number, next: Partial<SAMNode>) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Mobility</Heading>
      <p className={styles.sectionLead}>Set the mobility /24, deterministic SAM inner CIDR, and per-node capture metadata.</p>
      <div className={styles.grid}>
        <TextField label="Mobility prefix" value={state.mobilityPrefix} onChange={(mobilityPrefix) => update({mobilityPrefix})} />
        <TextField label="SAM inner CIDR" value={state.innerCIDR} onChange={(innerCIDR) => update({innerCIDR})} />
      </div>
      <div className={styles.tableWrap}>
        <table className={styles.nodeTable}>
          <thead>
            <tr>
              <th>Node</th>
              <th>Capture interface</th>
              <th>Static owned /32s</th>
              <th>Provider ref</th>
              <th>Placement group</th>
              <th>Priority</th>
              <th>WG public key</th>
            </tr>
          </thead>
          <tbody>
            {state.nodes.map((node, index) => (
              <tr key={`${node.nodeRef}-${index}`}>
                <td>{node.nodeRef || `node-${index + 1}`}</td>
                <td><input className={styles.input} value={node.captureInterface ?? ''} onChange={(event) => updateNode(index, {captureInterface: event.target.value})} /></td>
                <td>
                  {node.role === 'onprem' ? (
                    <input
                      className={styles.input}
                      value={(node.staticOwnedAddresses ?? []).join(', ')}
                      onChange={(event) => updateNode(index, {staticOwnedAddresses: splitCommaList(event.target.value)})}
                    />
                  ) : (
                    <span className={styles.muted}>provider observed</span>
                  )}
                </td>
                <td>
                  {node.role === 'cloud' ? (
                    <input className={styles.input} value={node.providerRef ?? ''} onChange={(event) => updateNode(index, {providerRef: event.target.value})} />
                  ) : (
                    <span className={styles.muted}>none</span>
                  )}
                </td>
                <td>
                  {node.role === 'cloud' ? (
                    <input className={styles.input} value={node.placementGroup ?? ''} onChange={(event) => updateNode(index, {placementGroup: event.target.value})} />
                  ) : (
                    <span className={styles.muted}>none</span>
                  )}
                </td>
                <td>
                  {node.role === 'cloud' ? (
                    <input
                      className={styles.input}
                      min={0}
                      type="number"
                      value={node.placementPriority ?? 10}
                      onChange={(event) => updateNode(index, {placementPriority: Number(event.target.value)})}
                    />
                  ) : (
                    <span className={styles.muted}>none</span>
                  )}
                </td>
                <td><input className={styles.input} value={node.wgPublicKey} onChange={(event) => updateNode(index, {wgPublicKey: event.target.value})} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

function K8SBGPStep({
  state,
  update,
}: {
  state: K8SWizardState;
  update: (next: PartialK8SWizardState) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Kubernetes BGP</Heading>
      <p className={styles.sectionLead}>Peer routerd with a Kubernetes route speaker or route server.</p>
      <div className={styles.grid}>
        <TextField label="Router name" value={state.routerName} onChange={(routerName) => update({routerName})} />
        <TextField label="BGP router resource" value={state.bgpRouterName} onChange={(bgpRouterName) => update({bgpRouterName})} />
        <TextField label="BGP peer resource" value={state.bgpPeerName} onChange={(bgpPeerName) => update({bgpPeerName})} />
        <label className={styles.field}>
          <span className={styles.label}>Session type</span>
          <select
            className={styles.select}
            value={state.sessionType}
            onChange={(event) => {
              const sessionType = event.target.value as K8SBGPSessionType;
              update({
                sessionType,
                peerASN: sessionType === 'ibgp' ? state.localASN : state.peerASN,
              });
            }}>
            <option value="ebgp">eBGP</option>
            <option value="ibgp">iBGP</option>
          </select>
        </label>
        <NumberField label="Local ASN" value={state.localASN} min={1} max={4294967295} onChange={(localASN) => update({localASN, peerASN: state.sessionType === 'ibgp' ? localASN : state.peerASN})} />
        <NumberField label="Peer ASN" value={state.peerASN} min={1} max={4294967295} onChange={(peerASN) => update({peerASN})} />
        <TextField label="Router ID" value={state.routerID} onChange={(routerID) => update({routerID})} />
        <TextField label="Listen address" value={state.listenAddress ?? ''} onChange={(listenAddress) => update({listenAddress})} />
        <TextField
          label="Peer addresses"
          hint="Comma-separated Kubernetes speaker or route-server addresses."
          value={state.peerAddresses.join(', ')}
          onChange={(value) => update({peerAddresses: splitCommaList(value)})}
        />
        <NumberField label="eBGP multihop" value={state.ebgpMultihop} min={0} max={255} onChange={(ebgpMultihop) => update({ebgpMultihop})} />
      </div>
    </>
  );
}

function K8SRoutesStep({
  state,
  update,
}: {
  state: K8SWizardState;
  update: (next: PartialK8SWizardState) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Routes</Heading>
      <p className={styles.sectionLead}>Constrain what routerd accepts from Kubernetes and what it advertises back.</p>
      <div className={styles.grid}>
        <TextField
          label="Import prefixes"
          hint="Pod, Service, or VIP prefixes accepted from Kubernetes."
          value={state.importPrefixes.join(', ')}
          onChange={(value) => update({importPrefixes: splitCommaList(value)})}
        />
        <TextField
          label="Export prefixes"
          hint="Local LAN or VIP prefixes advertised to Kubernetes."
          value={state.exportPrefixes.join(', ')}
          onChange={(value) => update({exportPrefixes: splitCommaList(value)})}
        />
        <label className={styles.field}>
          <span className={styles.label}>Timer profile</span>
          <select className={styles.select} value={state.timersProfile} onChange={(event) => update({timersProfile: event.target.value as K8SBGPTimerProfile})}>
            <option value="default">default</option>
            <option value="fast">fast</option>
            <option value="slow">slow</option>
          </select>
        </label>
        <label className={styles.field}>
          <span className={styles.label}>Convergence profile</span>
          <select className={styles.select} value={state.convergenceProfile} onChange={(event) => update({convergenceProfile: event.target.value as K8SBGPConvergenceProfile})}>
            <option value="default">default</option>
            <option value="fast">fast</option>
            <option value="stable">stable</option>
          </select>
        </label>
        <div className={`${styles.checks} ${styles.wide}`}>
          <CheckField
            checked={state.redistributeConnected}
            label="Redistribute connected"
            description="Allows connected routes that match the export prefixes to be advertised."
            onChange={(redistributeConnected) => update({redistributeConnected})}
          />
          <CheckField
            checked={state.redistributeStatic}
            label="Redistribute static"
            description="Allows static routes that match the export prefixes to be advertised."
            onChange={(redistributeStatic) => update({redistributeStatic})}
          />
        </div>
      </div>
    </>
  );
}

function InterfacesStep({
  state,
  update,
}: {
  state: HomeRouterWizardState;
  update: (next: PartialHomeRouterWizardState) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Interfaces</Heading>
      <p className={styles.sectionLead}>Name the router and map resource aliases to host NIC names.</p>
      <div className={styles.grid}>
        <TextField label="Router name" value={state.routerName} onChange={(routerName) => update({routerName})} />
        <TextField label="WAN NIC" value={state.interfaces.wan} onChange={(wan) => update({interfaces: {wan}})} />
        <TextField
          label="LAN NICs"
          hint="Comma-separated host NIC names."
          value={state.interfaces.lans.join(', ')}
          onChange={(value) => update({interfaces: {lans: splitList(value)}})}
        />
        <TextField
          label="Guest NIC"
          hint="Used only when guest mode is enabled."
          value={state.interfaces.guest ?? ''}
          onChange={(guest) => update({interfaces: {guest}})}
        />
      </div>
    </>
  );
}

function WanStep({
  state,
  update,
}: {
  state: HomeRouterWizardState;
  update: (next: PartialHomeRouterWizardState) => void;
}): JSX.Element {
  const staticMode = state.wan.mode === 'static';
  const pppoeMode = state.wan.mode === 'pppoe';
  const dsliteMode = state.wan.mode === 'dslite';
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>WAN</Heading>
      <p className={styles.sectionLead}>Select the access method and optional liveness checks.</p>
      <div className={styles.grid}>
        <label className={styles.field}>
          <span className={styles.label}>WAN mode</span>
          <select
            className={styles.select}
            value={state.wan.mode}
            onChange={(event) => update({wan: {mode: event.target.value as HomeWanMode}})}>
            {wanModes.map((mode) => (
              <option key={mode.value} value={mode.value}>{mode.label}</option>
            ))}
          </select>
        </label>
        <CheckField
          checked={state.wan.healthCheck}
          label="Internet health check"
          description="Adds an IPv4 TCP health check to 1.1.1.1:443."
          onChange={(healthCheck) => update({wan: {healthCheck}})}
        />
        {staticMode && (
          <>
            <TextField label="Static WAN address" value={state.wan.staticAddress ?? ''} onChange={(staticAddress) => update({wan: {staticAddress}})} />
            <TextField label="Static gateway" value={state.wan.staticGateway ?? ''} onChange={(staticGateway) => update({wan: {staticGateway}})} />
          </>
        )}
        {pppoeMode && (
          <>
            <TextField label="PPPoE username" value={state.wan.pppoeUsername ?? ''} onChange={(pppoeUsername) => update({wan: {pppoeUsername}})} />
            <TextField label="Password file" value={state.wan.pppoePasswordFile ?? ''} onChange={(pppoePasswordFile) => update({wan: {pppoePasswordFile}})} />
          </>
        )}
        {dsliteMode && (
          <>
            <TextField label="AFTR FQDN" value={state.wan.dsliteAFTRFQDN ?? ''} onChange={(dsliteAFTRFQDN) => update({wan: {dsliteAFTRFQDN}})} />
            <CheckField
              checked={state.wan.ipv6PD}
              label="DHCPv6 prefix delegation"
              description="Adds PD, a delegated LAN address, and DS-Lite source derivation."
              onChange={(ipv6PD) => update({wan: {ipv6PD}, lan: {raDhcpv6: ipv6PD ? state.lan.raDhcpv6 : false}})}
            />
          </>
        )}
        {!dsliteMode && (
          <CheckField
            checked={state.wan.ipv6PD}
            label="DHCPv6 prefix delegation"
            description="Adds WAN PD and a delegated LAN IPv6 address."
            onChange={(ipv6PD) => update({wan: {ipv6PD}, lan: {raDhcpv6: ipv6PD ? state.lan.raDhcpv6 : false}})}
          />
        )}
      </div>
    </>
  );
}

function LanStep({
  state,
  update,
}: {
  state: HomeRouterWizardState;
  update: (next: PartialHomeRouterWizardState) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>LAN</Heading>
      <p className={styles.sectionLead}>Choose LAN-side addressing and services.</p>
      <div className={styles.grid}>
        <TextField label="LAN address" value={state.lan.address} onChange={(address) => update({lan: {address}})} />
        <TextField label="DNS domain" value={state.lan.domain} onChange={(domain) => update({lan: {domain}})} />
        <div className={`${styles.checks} ${styles.wide}`}>
          <CheckField checked={state.lan.dhcpv4} label="DHCPv4 server" description="Adds a LAN address pool and router DNS options." onChange={(dhcpv4) => update({lan: {dhcpv4}})} />
          <CheckField checked={state.lan.dns} label="DNS resolver" description="Adds local DNS zone, cache, and public upstream forwarders." onChange={(dns) => update({lan: {dns}})} />
          <CheckField checked={state.lan.nat44} label="NAT44 masquerade" description="Adds outbound IPv4 masquerade for the LAN prefix." onChange={(nat44) => update({lan: {nat44}})} />
          <CheckField checked={state.lan.firewallZones} label="Firewall zones" description="Adds trust/untrust zones and the default home policy." onChange={(firewallZones) => update({lan: {firewallZones}})} />
          <CheckField checked={state.lan.guest} label="Guest mode" description="Adds a guest interface and client isolation policy." onChange={(guest) => update({lan: {guest}})} />
          <CheckField
            checked={state.lan.raDhcpv6}
            disabled={!state.wan.ipv6PD}
            label="RA + DHCPv6"
            description="Requires DHCPv6 prefix delegation."
            onChange={(raDhcpv6) => update({lan: {raDhcpv6}})}
          />
        </div>
      </div>
    </>
  );
}

function HaStep({
  state,
  update,
}: {
  state: HomeRouterWizardState;
  update: (next: PartialHomeRouterWizardState) => void;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>HA</Heading>
      <p className={styles.sectionLead}>Enable a LAN virtual address for a two-router pair.</p>
      <div className={styles.grid}>
        <div className={styles.wide}>
          <CheckField checked={state.ha.vrrp} label="VRRP virtual address" description="Adds a VirtualAddress resource on the LAN interface." onChange={(vrrp) => update({ha: {vrrp}})} />
        </div>
        <TextField label="Virtual IPv4 address" value={state.ha.virtualAddress ?? ''} onChange={(virtualAddress) => update({ha: {virtualAddress}})} />
        <TextField label="Peer address" value={state.ha.peer ?? ''} onChange={(peer) => update({ha: {peer}})} />
        <NumberField label="VRID" value={state.ha.virtualRouterID ?? 10} min={1} max={255} onChange={(virtualRouterID) => update({ha: {virtualRouterID}})} />
        <NumberField label="Priority" value={state.ha.priority ?? 150} min={1} max={254} onChange={(priority) => update({ha: {priority}})} />
      </div>
    </>
  );
}

function OutputStep({
  activeOutputName,
  copyState,
  copyYaml,
  downloadYaml,
  outputNames,
  resourceCount,
  setSelectedOutput,
  summary,
  yaml,
}: {
  activeOutputName: string;
  copyState: 'idle' | 'copied' | 'failed';
  copyYaml: () => void;
  downloadYaml: () => void;
  outputNames: string[];
  resourceCount: number;
  setSelectedOutput: (name: string) => void;
  summary: Array<[string, string]>;
  yaml: string;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Output</Heading>
      <p className={styles.sectionLead}>Copy or download the generated `router.yaml`.</p>
      <div className={styles.outputGrid}>
        <pre className={styles.code}><code>{yaml}</code></pre>
        <aside className={styles.outputMeta}>
          {outputNames.length > 1 && (
            <label className={styles.field}>
              <span className={styles.label}>Node YAML</span>
              <select className={styles.select} value={activeOutputName} onChange={(event) => setSelectedOutput(event.target.value)}>
                {outputNames.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
            </label>
          )}
          <dl>
            {summary.map(([label, value]) => (
              <React.Fragment key={label}>
                <dt>{label}</dt>
                <dd>{value}</dd>
              </React.Fragment>
            ))}
            <dt>File</dt>
            <dd>{activeOutputName}</dd>
            <dt>Resources</dt>
            <dd>{resourceCount}</dd>
            <dt>Schema</dt>
            <dd>routerd-config-v1alpha1</dd>
          </dl>
          {copyState === 'copied' && <div className={styles.notice}>Copied to clipboard.</div>}
          {copyState === 'failed' && <div className={styles.notice}>Clipboard access failed. Select the YAML and copy it manually.</div>}
          <div className={styles.actionGroup} style={{marginTop: '14px'}}>
            <button className={`${styles.button} ${styles.buttonSecondary}`} onClick={copyYaml} type="button">Copy YAML</button>
            <button className={`${styles.button} ${styles.buttonPrimary}`} onClick={downloadYaml} type="button">Download</button>
          </div>
        </aside>
      </div>
    </>
  );
}

function TextField({
  hint,
  label,
  onChange,
  value,
}: {
  hint?: string;
  label: string;
  onChange: (value: string) => void;
  value: string;
}): JSX.Element {
  return (
    <label className={styles.field}>
      <span className={styles.label}>{label}</span>
      <input className={styles.input} value={value} onChange={(event) => onChange(event.target.value)} />
      {hint && <span className={styles.hint}>{hint}</span>}
    </label>
  );
}

function NumberField({
  label,
  max,
  min,
  onChange,
  value,
}: {
  label: string;
  max: number;
  min: number;
  onChange: (value: number) => void;
  value: number;
}): JSX.Element {
  return (
    <label className={styles.field}>
      <span className={styles.label}>{label}</span>
      <input
        className={styles.input}
        max={max}
        min={min}
        type="number"
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </label>
  );
}

function CheckField({
  checked,
  description,
  disabled,
  label,
  onChange,
}: {
  checked: boolean;
  description: string;
  disabled?: boolean;
  label: string;
  onChange: (value: boolean) => void;
}): JSX.Element {
  return (
    <label className={styles.check}>
      <input
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
      <span>
        <strong>{label}</strong>
        <span>{description}</span>
      </span>
    </label>
  );
}

type PartialHomeRouterWizardState = {
  routerName?: string;
  interfaces?: Partial<HomeRouterWizardState['interfaces']>;
  wan?: Partial<HomeRouterWizardState['wan']>;
  lan?: Partial<HomeRouterWizardState['lan']>;
  ha?: Partial<HomeRouterWizardState['ha']>;
};

type PartialSAMWizardState = {
  name?: string;
  mobilityPrefix?: string;
  innerCIDR?: string;
  bgpASN?: number;
  routeReflectorNodeRef?: string;
  nodes?: SAMNode[];
};

type PartialK8SWizardState = {
  routerName?: string;
  bgpRouterName?: string;
  bgpPeerName?: string;
  sessionType?: K8SBGPSessionType;
  localASN?: number;
  peerASN?: number;
  routerID?: string;
  listenAddress?: string;
  peerAddresses?: string[];
  importPrefixes?: string[];
  exportPrefixes?: string[];
  redistributeConnected?: boolean;
  redistributeStatic?: boolean;
  ebgpMultihop?: number;
  timersProfile?: K8SBGPTimerProfile;
  convergenceProfile?: K8SBGPConvergenceProfile;
};

function splitList(value: string): string[] {
  const items = splitCommaList(value);
  return items.length > 0 ? items : DEFAULT_HOME_ROUTER_STATE.interfaces.lans;
}

function splitCommaList(value: string): string[] {
  return value.split(',').map((item) => item.trim()).filter(Boolean);
}

function newSAMNode(index: number): SAMNode {
  const underlay = `10.99.0.${index}`;
  return {
    nodeRef: `edge-${index}`,
    site: `site-${index}`,
    role: 'cloud',
    underlayIPv4: underlay,
    wgEndpoint: `edge-${index}.example.net:51820`,
    wgPublicKey: `\${EDGE_${index}_WG_PUBLIC_KEY}`,
    routerID: underlay,
    provider: 'aws',
    providerRef: 'aws-lab',
    captureInterface: 'ens5',
    placementGroup: 'aws-edge',
    placementPriority: index * 10,
  };
}
