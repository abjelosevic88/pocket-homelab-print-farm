import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AppSlug, AppState, ConnMode, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'
import { APP_INFO, STATE_WORDS } from '../lib/apps'
import { MATERIALS } from '../lib/farm'

type TabId = 'general' | 'notifications' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Costs, filament, how prints start and end — and dates and the app title.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'What reaches your phone: a print done or failed, a spool running low, upkeep due, a printer not answering.' },
  { id: 'apps', label: 'Connected apps', icon: 'link', sub: 'Files, Inventory, Money, Invoicing, Contacts, Tasks, Calendar, Chat, Parcels, Projects, Home and Alerts.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who helps you: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box. Each person has their own printers, models and filament.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Siri Shortcuts, scripts and other tools.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export everything, bring an export back, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Costs" icon="coins" sub="What each print cost: the filament (from the spool's price), the electricity (the printer's watts) and wear (per print hour, on each printer).">
        <SettingRow title="Currency"><input value={f.currency} onChange={e => setF({ ...f, currency: e.target.value.toUpperCase().slice(0, 3) })} placeholder="EUR" style={{ maxWidth: 110 }} /></SettingRow>
        <SettingRow title="Electricity" help="What a kWh costs you."><div className="input-suffix" style={{ maxWidth: 180 }}><input type="number" step={0.01} min={0} value={f.kwh_price} onChange={e => setF({ ...f, kwh_price: Number(e.target.value) })} /><span>{f.currency}/kWh</span></div></SettingRow>
        <SettingRow title="Price to sell at" help="The cost plus this much, as a suggestion on each print."><div className="input-suffix" style={{ maxWidth: 140 }}><input type="number" min={0} value={f.markup} onChange={e => setF({ ...f, markup: Number(e.target.value) })} /><span>%</span></div></SettingRow>
      </Section>
      <Section title="Filament" icon="disc">
        <SettingRow title="Take what a print used off the spool" help="When a print ends, its grams come off the spool loaded in the printer."><Switch checked={f.auto_deduct} onChange={v => setF({ ...f, auto_deduct: v })} label="Take off" /></SettingRow>
        <SettingRow title="A spool is running low under"><div className="input-suffix" style={{ maxWidth: 140 }}><input type="number" min={0} value={f.low_grams} onChange={e => setF({ ...f, low_grams: Number(e.target.value) })} /><span>g</span></div></SettingRow>
        <SettingRow title="Usual material"><Select ariaLabel="Material" value={f.default_material} onChange={v => setF({ ...f, default_material: v })} options={MATERIALS.map(m => ({ value: m, label: m }))} /></SettingRow>
      </Section>
      <Section title="Printing" icon="printer" sub={f.poll_seconds ? `Connected printers are asked how they are every ${f.poll_seconds} seconds.` : 'Connected printers are only asked when a page opens (PRINTER_POLL=0).'}>
        <SettingRow title="Send the G-code to connected printers" help="Starting a job uploads its G-code and the printer starts it. Off: start on the printer yourself; Print Farm sees it and follows."><Switch checked={f.send_gcode} onChange={v => setF({ ...f, send_gcode: v })} label="Send G-code" /></SettingRow>
        <SettingRow title="Wait for “the bed is clear”" help="After a print, a printer takes nothing new until you say the bed is clear."><Switch checked={f.need_clear} onChange={v => setF({ ...f, need_clear: v })} label="Bed clear" /></SettingRow>
        <SettingRow title="Start the next job by itself" help="On a connected printer that is free and clear, with the right filament. Only for printers that clear their own bed (or when you are there)."><Switch checked={f.auto_start} disabled={!f.send_gcode} onChange={v => setF({ ...f, auto_start: v })} label="Auto start" /></SettingRow>
        <SettingRow title="A print stopped on the printer counts as"><Segmented size="sm" value={f.default_fail} onChange={v => setF({ ...f, default_fail: v })} options={[{ value: 'failed', label: 'Failed' }, { value: 'cancelled', label: 'Stopped' }]} /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where messages go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/printfarm" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="What to push" icon="bell">
        <SettingRow title="A print is done" help="With what to do next (take it off the bed). In Pocket Alerts too, when it is connected."><Switch checked={f.notify_done} onChange={v => setF({ ...f, notify_done: v })} label="Done" /></SettingRow>
        <SettingRow title="A print failed or was stopped"><Switch checked={f.notify_failed} onChange={v => setF({ ...f, notify_failed: v })} label="Failed" /></SettingRow>
        <SettingRow title="A spool is running low"><Switch checked={f.notify_low} onChange={v => setF({ ...f, notify_low: v })} label="Low" /></SettingRow>
        <SettingRow title="Upkeep is due" help="Once a day, at the hour below."><Switch checked={f.notify_upkeep} onChange={v => setF({ ...f, notify_upkeep: v })} label="Upkeep" /></SettingRow>
        <SettingRow title="A printer stops answering or shows an error"><Switch checked={f.notify_offline} onChange={v => setF({ ...f, notify_offline: v })} label="Offline" /></SettingRow>
        <SettingRow title="Upkeep reminders go at" help="Server time."><Select ariaLabel="Hour" value={String(f.notify_hour)} onChange={v => setF({ ...f, notify_hour: Number(v) })} options={HOURS} /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test push</button>
        {status('notify')}
      </div>
    </div>
  )
}

const APPS: { slug: AppSlug; title: string; sub: string; token: string; port: string }[] = [
  { slug: 'files', title: 'Pocket Files — models in, copies out', sub: 'Bring STL, 3MF and G-code files in from your files; keep copies of model files and photos of prints there.', token: 'Pocket Files → Settings → API tokens', port: '8187' },
  { slug: 'inventory', title: 'Pocket Inventory — what you printed', sub: 'Put finished prints on your inventory, or add to how many there are.', token: 'Pocket Inventory → Settings → API tokens', port: '8186' },
  { slug: 'money', title: 'Pocket Money — filament bought', sub: 'Write down a spool you bought as an expense.', token: 'Pocket Money → Settings → API tokens', port: '8178' },
  { slug: 'invoicing', title: 'Pocket Invoicing — prints sold', sub: 'Make a draft invoice for a job from its price and quantity.', token: 'Pocket Invoicing → Settings → API tokens', port: '8183' },
  { slug: 'contacts', title: 'Pocket Contacts — who it is for', sub: 'Choose who a job is for from your address book.', token: 'Pocket Contacts → Settings → API tokens', port: '8191' },
  { slug: 'tasks', title: 'Pocket Tasks — upkeep and jobs', sub: 'A task when a printer’s upkeep is due (ticked off when you say it is done), or for a job with a due date.', token: 'Pocket Tasks → Settings → API tokens', port: '8181' },
  { slug: 'calendar', title: 'Pocket Calendar — when a long print is done', sub: 'The time a long print will be done, in your calendar.', token: 'Pocket Calendar → Settings → API tokens', port: '8190' },
  { slug: 'chat', title: 'Pocket Chat — show what you printed', sub: 'Share a finished print, with its photo, to a chat.', token: 'Pocket Chat → Settings → API tokens', port: '8247' },
  { slug: 'parcels', title: 'Pocket Parcels — filament on the way', sub: 'Filament you ordered that is on its way shows on the Filament page.', token: 'Pocket Parcels → Settings → API tokens', port: '8214' },
  { slug: 'projects', title: 'Pocket Projects — an issue from a print', sub: 'A part for a project, or a failure to look into, as an issue.', token: 'Pocket Projects → Settings → API tokens', port: '8197' },
  { slug: 'home', title: 'Pocket Home — the printer as an appliance', sub: 'Link a printer to its appliance; upkeep and repairs go into its log there.', token: 'Pocket Home → Settings → API tokens', port: '8229' },
  { slug: 'alerts', title: 'Pocket Alerts — your alerts inbox', sub: 'A print done or failed, a spool running low, upkeep due, a printer not answering.', token: 'Pocket Alerts → Settings → API tokens', port: '8217' },
]
function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: states } = useAsync(() => api.get<{ apps: AppState[] }>(`${V1}/apps`).then(r => r.apps).catch(() => [] as AppState[]), [APPS.map(a => f?.[`${a.slug}_mode`]).join()])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const set = (k: string, v: string) => setF({ ...f, [k]: v } as SettingsT)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Print Farm reads or adds things in them when you press a button (and, when you switch it on below, when a spool is bought, a long print starts or upkeep is due), and everything here works without them. Through <b>Pocket Core</b> Print Farm acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => {
        const mode = f[`${a.slug}_mode`]
        const via = f[`${a.slug}_via`]
        const name = APP_INFO[a.slug].name
        const st = states?.find(x => x.slug === a.slug)
        return (
          <Section key={a.slug} title={a.title} icon={APP_INFO[a.slug].icon} actions={st && st.state !== 'on' && st.state !== 'off' ? <span className={`badge ${st.state === 'partly' ? 'ok' : 'warn'}`}>{STATE_WORDS[st.state]}</span> : connBadge(eff(mode), via)} sub={a.sub}>
            <Connection app={name} mode={mode} via={via} detail={f[`${a.slug}_detail`]} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
              onMode={m => save({ [`${a.slug}_mode`]: m } as Partial<SettingsT>)} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' } as Partial<SettingsT>)}
              test={<><button type="button" className="btn sm" onClick={() => run(a.slug, f[`${a.slug}_url`], f[`${a.slug}_token`])}>Test connection</button>{status(a.slug)}</>}
              direct={<div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f[`${a.slug}_url`]} onChange={e => set(`${a.slug}_url`, e.target.value)} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
                <Field label="API token" help={a.token}><input value={f[`${a.slug}_token`]} onChange={e => set(`${a.slug}_token`, e.target.value)} autoComplete="off" /></Field>
              </div>} />
            {st?.fix && st.state !== 'on' && <p className="muted small"><Icon name="info" size={12} className="inline-ico" /> {st.fix}.</p>}
          </Section>
        )
      })}
      <WhereThingsGo f={f} setF={setF} />
      <Section title="Other Pocket apps" icon="layers" sub="Print Farm offers your printing to the rest of the box through Pocket Core: Pocket Day shows what was printed, Pocket Search finds models and prints, Pocket AI can tell you how the farm is doing or put a job in the queue. Switched off until you allow them in Pocket Core → Links.">
        <dl className="set-dl">
          <div><dt>printfarm:read</dt><dd className="muted">Your printers and how they are doing, the queue, models, spools, prints with what they cost, upkeep and the numbers.</dd></div>
          <div><dt>printfarm:write</dt><dd className="muted">Add a job to the queue, a model from a link, a print made, a spool bought or weighed; that app may change only what it added, and never starts, pauses or stops a printer.</dd></div>
          <div><dt>events:read</dt><dd className="muted"><code>printfarm.print_started</code>, <code>print_finished</code>, <code>print_failed</code>, <code>job_added</code>, <code>job_done</code>, <code>spool_low</code>, <code>upkeep_due</code>, <code>model_added</code> — for Pocket Flows.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Only when you press its button: on a failed print it says what probably went wrong and what to change; on a model it suggests settings to start from, from your own prints that worked. It never changes anything by itself." >
        <SettingRow title="AI buttons" help="Off: the buttons are gone and nothing is sent to an AI."><Switch checked={f.ai_help} onChange={v => save({ ai_help: v })} label="AI buttons" /></SettingRow>
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Print Farm does not reach ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own printers, models, filament, settings and connections. Admins manage accounts; they never see other people's printing.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account goes, with their printers, models, filament and history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      setUser(await api.put(`${V1}/auth/me`, f))
      setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email" help="The same email as in Pocket Core, so signing in finds your account."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/summary</code>) or add a job from a Siri Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone Shortcut)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /summary</code></dt><dd className="muted">Each printer now (printing what, time left), the queue, the week, low filament — for Homepage.</dd></div>
          <div><dt><code>GET /farm</code></dt><dd className="muted">Every printer with its live state, temperatures, the print on it and the spool in it.</dd></div>
          <div><dt><code>POST /jobs</code></dt><dd className="muted"><code>{'{"model_id": 4, "quantity": 2, "material": "PETG"}'}</code> puts a job in the queue — a Shortcut on your phone.</dd></div>
          <div><dt><code>POST /printers/ID/control</code></dt><dd className="muted"><code>{'{"action": "pause"}'}</code> (pause, resume, cancel, clear, refresh) — a Stream Deck button.</dd></div>
          <div><dt><code>POST /models/upload</code></dt><dd className="muted">STL, 3MF, OBJ, STEP or G-code as a form (<code>file</code>) — from a slicer’s post-processing script.</dd></div>
          <div><dt><code>POST /spools/ID/weigh</code></dt><dd className="muted"><code>{'{"gross_g": 812}'}</code> from a smart scale.</dd></div>
          <div><dt><code>GET /search?q=…</code></dt><dd className="muted">Models (and their files), printers, filament, prints and jobs.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">What was printed on a day.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { user } = useApp()
  const { toast } = useToast()
  const file = React.useRef<HTMLInputElement>(null)
  const bring = async (fl: File) => {
    try {
      const res = await fetch(`${V1}/import`, { method: 'POST', body: await fl.text(), headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin' })
      const j = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(j.error || res.statusText)
      toast(j.text || 'Brought in')
    } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Section title="Export" icon="archive" sub="Everything you made as one JSON file — printers (without their API keys), filament and spools, models with what was read from their files, the queue, every print and the upkeep — or a table of every print (CSV). The model files and photos themselves are in the data folder (and the box backup).">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Prints as CSV</a></div>
      </Section>
      <Section title="Bring a file in" icon="upload" sub="A Pocket Print Farm export (also from another box): its printers, filament, models, queue and history come back as yours. The same file twice adds nothing twice. Model files need adding again (or come back with a backup of the box).">
        <button type="button" className="btn" onClick={() => file.current?.click()}><Icon name="upload" size={15} />Bring a file…</button>
        <input ref={file} type="file" hidden accept=".json,application/json" onChange={e => { const fl = e.target.files?.[0]; e.target.value = ''; if (fl) bring(fl) }} />
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-printfarm.db</code> in the data folder.</p>
        <a className="btn" href={`${V1}/backup`}><Icon name="database" size={15} />Download database</a>
      </Section>}
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Printers', counts.printers, 'printer'], ['Models', counts.models, 'package'], ['Files', counts.model_files, 'file'], ['Spools', counts.spools, 'disc'], ['Jobs', counts.jobs, 'list'], ['Prints', counts.prints, 'history']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Print Farm</h3>
          <p className="muted small">Your 3D printers in one place: live state from Klipper, OctoPrint and PrusaLink, a queue, a model library with a 3D view, filament spools, every print with its cost, and upkeep. One Go binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-print-farm" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Calendar, Contacts, Family, Email, Chat, Photos, Notes, Files, Voice, Tasks, Alerts, Blog, Weather, Bookshelf, Music, Movement, Health, Wealth, Pocket AI and more.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}

function WhereThingsGo({ f, setF }: { f: SettingsT; setF: (s: SettingsT) => void }) {
  const { data: chats } = useAsync(() => f.chat_via ? api.get<{ conversations: { id: string; name: string }[] }>(`${V1}/apps/chats`).then(r => r.conversations).catch(() => []) : Promise.resolve([]), [f.chat_via])
  const { data: lists } = useAsync(() => f.tasks_via ? api.get<{ lists: { id: string; name: string }[] }>(`${V1}/apps/tasks/lists`).then(r => r.lists).catch(() => []) : Promise.resolve([]), [f.tasks_via])
  const { data: locs } = useAsync(() => f.inventory_via ? api.get<{ locations: { id: number; name: string }[] }>(`${V1}/apps/inventory/locations`).then(r => r.locations).catch(() => []) : Promise.resolve([]), [f.inventory_via])
  const { data: money } = useAsync(() => f.money_via ? api.get<{ accounts: { id: string; name: string; currency: string }[]; categories: { id: string; name: string }[] }>(`${V1}/apps/money`).catch(() => ({ accounts: [], categories: [] })) : Promise.resolve({ accounts: [], categories: [] }), [f.money_via])
  return (
    <Section title="Where things go" icon="layers">
      <SettingRow title="Long prints in Pocket Calendar" help={f.calendar_via ? 'The time a print longer than this will be done.' : 'Needs Pocket Calendar.'}>
        <Segmented size="sm" value={f.calendar_long} onChange={v => setF({ ...f, calendar_long: v })} options={[{ value: 0, label: 'Never', disabled: false }, { value: 4, label: '4 h', disabled: !f.calendar_via }, { value: 8, label: '8 h', disabled: !f.calendar_via }, { value: 16, label: '16 h', disabled: !f.calendar_via }]} />
      </SettingRow>
      <SettingRow title="Upkeep that is due becomes a task" help={f.tasks_via ? 'In Pocket Tasks; ticked off when you say it is done here.' : 'Needs Pocket Tasks.'}><Switch checked={f.tasks_upkeep} disabled={!f.tasks_via} onChange={v => setF({ ...f, tasks_upkeep: v })} label="Tasks" /></SettingRow>
      <SettingRow title="Tasks go to">
        <Select ariaLabel="List" value={f.tasks_list} onChange={v => setF({ ...f, tasks_list: v })} placeholder="The inbox" clearLabel="The inbox" disabled={!f.tasks_via} options={(lists || []).map(l => ({ value: l.id, label: l.name }))} />
      </SettingRow>
      <SettingRow title="Finished prints go on the inventory at" help={f.inventory_via ? 'A place in Pocket Inventory (a new name makes it).' : 'Needs Pocket Inventory.'}>
        <Select ariaLabel="Place" value={f.inventory_location} onChange={v => setF({ ...f, inventory_location: v })} placeholder="No place" clearLabel="No place" disabled={!f.inventory_via} options={(locs || []).map(l => ({ value: l.name, label: l.name }))} onCreate={v => v} createLabel="Use" />
      </SettingRow>
      <SettingRow title="A new spool with a price is an expense" help={f.money_via ? 'Written down in Pocket Money as you add it.' : 'Needs Pocket Money.'}><Switch checked={f.money_on_spool} disabled={!f.money_via} onChange={v => setF({ ...f, money_on_spool: v })} label="Money" /></SettingRow>
      <SettingRow title="From the account">
        <Select ariaLabel="Account" value={f.money_account} onChange={v => setF({ ...f, money_account: v })} placeholder="The first one" clearLabel="The first one" disabled={!f.money_via} options={(money?.accounts || []).map(a => ({ value: a.id, label: a.name, hint: a.currency }))} />
      </SettingRow>
      <SettingRow title="In the category">
        <Select ariaLabel="Category" value={f.money_category} onChange={v => setF({ ...f, money_category: v })} placeholder="None" clearLabel="None" disabled={!f.money_via} options={[...(money?.categories || []).map(c => ({ value: c.name, label: c.name })), ...(f.money_category && !(money?.categories || []).some(c => c.name === f.money_category) ? [{ value: f.money_category, label: f.money_category }] : [])]} />
      </SettingRow>
      <SettingRow title="Shared prints go to" help="The Pocket Chat conversation; empty: a note to yourself.">
        {chats?.length ? <Select ariaLabel="Chat" value={f.chat_to} onChange={v => setF({ ...f, chat_to: v })} placeholder="Me" clearLabel="Me" options={chats.map(c => ({ value: c.id, label: c.name || c.id }))} /> : <input value={f.chat_to} onChange={e => setF({ ...f, chat_to: e.target.value })} placeholder="me" disabled={!f.chat_via} />}
      </SettingRow>
      <SettingRow title="Files folder" help="Where copies of model files and photos go in Pocket Files."><input value={f.files_folder} onChange={e => setF({ ...f, files_folder: e.target.value })} disabled={!f.files_via} /></SettingRow>
    </Section>
  )
}
