// SPDX-License-Identifier: BSD-3-Clause

import React, {useMemo, useState} from 'react';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import {
  DEFAULT_HOME_ROUTER_STATE,
  buildHomeRouterYaml,
  mergeHomeRouterState,
  type HomeRouterWizardState,
  type HomeWanMode,
} from '../lib/routerdWizard';
import styles from './wizard.module.css';

const steps = ['Interfaces', 'WAN', 'LAN', 'HA', 'Output'] as const;
type Step = (typeof steps)[number];

const wanModes: Array<{value: HomeWanMode; label: string}> = [
  {value: 'dhcpv4', label: 'DHCPv4 client'},
  {value: 'pppoe', label: 'PPPoE'},
  {value: 'dslite', label: 'DS-Lite'},
  {value: 'static', label: 'Static IPv4'},
];

export default function WizardPage(): JSX.Element {
  const [step, setStep] = useState<Step>('Interfaces');
  const [state, setState] = useState<HomeRouterWizardState>(() => mergeHomeRouterState());
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const yaml = useMemo(() => buildHomeRouterYaml(state), [state]);
  const stepIndex = steps.indexOf(step);
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
    link.download = `${state.routerName || DEFAULT_HOME_ROUTER_STATE.routerName}.yaml`;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <Layout title="Config wizard" description="Generate a routerd home router configuration.">
      <main className={styles.wizardPage}>
        <div className={`container ${styles.shell}`}>
          <aside className={styles.sidebar}>
            <nav className={styles.stepList} aria-label="Wizard steps">
              {steps.map((item, index) => (
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
              <div><b>{state.wan.mode}</b> WAN</div>
              <div>{state.interfaces.lans.length} LAN interface{state.interfaces.lans.length === 1 ? '' : 's'}</div>
              <div>{resourceCount} generated resources</div>
            </div>
          </aside>

          <section className={styles.panel}>
            <div className={styles.header}>
              <Heading as="h1">routerd config wizard</Heading>
              <p>Build a Home Router `router.yaml` from the same typed builder that generates the CI fixtures.</p>
            </div>

            {step === 'Interfaces' && <InterfacesStep state={state} update={update} />}
            {step === 'WAN' && <WanStep state={state} update={update} />}
            {step === 'LAN' && <LanStep state={state} update={update} />}
            {step === 'HA' && <HaStep state={state} update={update} />}
            {step === 'Output' && (
              <OutputStep
                copyState={copyState}
                downloadYaml={downloadYaml}
                resourceCount={resourceCount}
                copyYaml={copyYaml}
                state={state}
                yaml={yaml}
              />
            )}

            <div className={styles.actions}>
              <button
                className={`${styles.button} ${styles.buttonSecondary}`}
                disabled={stepIndex === 0}
                onClick={() => setStep(steps[Math.max(stepIndex - 1, 0)])}
                type="button">
                Back
              </button>
              <div className={styles.actionGroup}>
                {step !== 'Output' && (
                  <button
                    className={`${styles.button} ${styles.buttonPrimary}`}
                    onClick={() => setStep(steps[Math.min(stepIndex + 1, steps.length - 1)])}
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
  copyState,
  copyYaml,
  downloadYaml,
  resourceCount,
  state,
  yaml,
}: {
  copyState: 'idle' | 'copied' | 'failed';
  copyYaml: () => void;
  downloadYaml: () => void;
  resourceCount: number;
  state: HomeRouterWizardState;
  yaml: string;
}): JSX.Element {
  return (
    <>
      <Heading as="h2" className={styles.sectionTitle}>Output</Heading>
      <p className={styles.sectionLead}>Copy or download the generated `router.yaml`.</p>
      <div className={styles.outputGrid}>
        <pre className={styles.code}><code>{yaml}</code></pre>
        <aside className={styles.outputMeta}>
          <dl>
            <dt>Router</dt>
            <dd>{state.routerName}</dd>
            <dt>WAN mode</dt>
            <dd>{state.wan.mode}</dd>
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

function splitList(value: string): string[] {
  const items = value.split(',').map((item) => item.trim()).filter(Boolean);
  return items.length > 0 ? items : DEFAULT_HOME_ROUTER_STATE.interfaces.lans;
}
