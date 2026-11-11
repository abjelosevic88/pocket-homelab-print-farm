// Filament: the spools on the shelf, what is left on each, and the kinds of filament.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useAppState, useOnChange, openAction } from '../lib/data'
import { grams, money } from '../lib/farm'
import type { Filament, Spool, SpoolLog, Print } from '../lib/types'
import { ago, fmtDate, fmtDateTime } from '../lib/format'
import { Empty, Field, Loading, MenuButton, Modal, Segmented, useToast, errMsg, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Bar, PrintBadge, Swatch } from '../components/bits'
import { FilamentDialog, SpoolDialog, WeighDialog } from '../components/dialogs'

type Parcel = { id: string; title: string; shop: string; status: string; expected_on: string; arrives_in: number | null; url: string }

export default function FilamentPage() {
  const { settings } = useApp()
  const { toast } = useToast()
  const state = useAppState()
  const [view, setView] = useState<'spools' | 'filaments' | 'empty'>('spools')
  const [spools, setSpools] = useState<{ spools: Spool[]; low: number; left_g: number; low_grams: number } | null>(null)
  const [fils, setFils] = useState<Filament[]>([])
  const [parcels, setParcels] = useState<Parcel[]>([])
  const [material, setMaterial] = useState('')
  const [edit, setEdit] = useState<Spool | 'new' | null>(null)
  const [weigh, setWeigh] = useState<Spool | null>(null)
  const [fil, setFil] = useState<Filament | 'new' | null>(null)
  const [open, setOpen] = useState<Spool | null>(null)
  const [spoolman, setSpoolman] = useState(false)
  const load = useCallback(() => {
    api.get<typeof spools>(`${V1}/spools${view === 'empty' ? '?archived=1' : ''}`).then(setSpools).catch(() => {})
    api.get<{ filaments: Filament[] }>(`${V1}/filaments`).then(r => setFils(r.filaments)).catch(() => {})
  }, [view])
  useEffect(load, [load])
  useOnChange(load)
  const parcelsOn = state('parcels', 'parcels:read').ok
  useEffect(() => { if (parcelsOn) api.get<{ parcels: Parcel[] }>(`${V1}/apps/parcels`).then(r => setParcels(r.parcels)).catch(() => {}) }, [parcelsOn])
  const cur = settings?.currency || 'EUR'
  const low = spools?.low_grams ?? 150
  const list = (spools?.spools || []).filter(s => (view === 'empty' ? s.archived : !s.archived) && (!material || s.material === material))
  const materials = [...new Set((spools?.spools || []).filter(s => !s.archived).map(s => s.material))].sort()
  const toMoney = async (s: Spool) => {
    try { const r = await api.post<{ url?: string; text?: string }>(`${V1}/spools/${s.id}/money`, {}); toast(r.text || 'Done', 'ok', openAction(r.url)); load() } catch (e) { toast(errMsg(e), 'err') }
  }
  const moneyOn = state('money', 'transactions:write').ok
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">Filament</h2><p className="muted">{spools ? `${spools.spools.filter(s => !s.archived).length} spools · ${grams(spools.left_g)} on the shelf${spools.low ? ` · ${spools.low} running low` : ''}` : ' '}</p></div>
        <Segmented size="sm" value={view} onChange={setView} options={[{ value: 'spools', label: 'Spools' }, { value: 'filaments', label: 'Kinds' }, { value: 'empty', label: 'Empty' }]} />
        <MenuButton label="More" align="right" items={(): MenuItem[] => [
          { label: 'A new kind of filament', icon: 'plus', onSelect: () => setFil('new') },
          { label: 'Bring spools over from Spoolman', icon: 'download', onSelect: () => setSpoolman(true) },
        ]}><Icon name="more" size={16} /></MenuButton>
        <button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add spools</button>
      </div>
      {parcels.length > 0 && <div className="attention">
        {parcels.map(p => <a key={p.id} href={p.url} target="_blank" rel="noreferrer" className="att-card">
          <Icon name="package" size={18} />
          <div className="grow min0"><b className="ellipsis block-line">On the way: {p.title}</b><span className="muted small">{[p.shop, p.status, p.expected_on && `expected ${fmtDate(p.expected_on)}`].filter(Boolean).join(' · ')}</span></div>
          <Icon name="external" size={14} />
        </a>)}
      </div>}
      {view !== 'filaments' && materials.length > 1 && <div className="chips">
        <button type="button" className={`chip ${!material ? 'on' : ''}`} onClick={() => setMaterial('')}>All</button>
        {materials.map(m => <button key={m} type="button" className={`chip ${material === m ? 'on' : ''}`} onClick={() => setMaterial(material === m ? '' : m)}>{m}</button>)}
      </div>}
      {!spools ? <Loading /> : view === 'filaments' ? (
        fils.length === 0 ? <Empty title="No filaments yet" icon="disc">Add a spool: its filament (brand, material, colour) comes with it.</Empty> : (
          <div className="card flush"><ul className="list">{fils.map(f => (
            <li key={f.id}><button type="button" className="list-row plain" onClick={() => setFil(f)}>
              <Swatch color={f.color} size={22} />
              <div className="grow min0"><div className="ellipsis"><b>{f.material}</b> {f.name} <span className="muted">{f.brand}</span></div>
                <div className="muted small">{[f.nozzle_temp && `${f.nozzle_temp} °C`, f.bed_temp && `bed ${f.bed_temp} °C`, f.price && `${money(f.price, cur)} a spool`, `${f.spools} ${f.spools === 1 ? 'spool' : 'spools'}, ${grams(f.left_grams)} left`].filter(Boolean).join(' · ')}</div></div>
              <Icon name="chevron-right" size={15} />
            </button></li>
          ))}</ul></div>
        )
      ) : list.length === 0 ? <Empty title={view === 'empty' ? 'No empty spools' : 'No spools on the shelf'} icon="disc">{view !== 'empty' && <>Add the spools you have; as prints finish, what they used comes off the spool in the printer.</>}</Empty> : (
        <div className="spool-grid">
          {list.map(s => {
            const frac = s.initial_g ? s.left_g / s.initial_g : 0
            return (
              <article key={s.id} className={`spool-card ${s.left_g < low && !s.archived ? 'low' : ''}`}>
                <button type="button" className="spool-main plain" onClick={() => setOpen(s)}>
                  <span className="spool-disc" style={{ '--sc': s.color || '#999' } as React.CSSProperties}><span style={{ transform: `scale(${0.42 + 0.58 * Math.max(0, Math.min(1, frac))})` }} /></span>
                  <div className="min0 grow">
                    <b className="ellipsis block-line">{s.material} {s.name}</b>
                    <span className="muted small ellipsis block-line">{s.brand}{s.location ? ` · ${s.location}` : ''}</span>
                  </div>
                </button>
                <div className="spool-left"><b className="tnum">{Math.round(s.left_g)} g</b><span className="muted small">of {Math.round(s.initial_g)}</span></div>
                <Bar value={frac} thin tone={s.left_g < low ? 'warn' : ''} />
                <div className="spool-foot">
                  {s.printer ? <span className="badge accent"><Icon name="printer" size={11} />{s.printer}</span> : <span className="muted small">{s.last_used_at ? `used ${ago(s.last_used_at)}` : 'not used yet'}</span>}
                  <span className="spacer" />
                  <button type="button" className="btn ghost sm" onClick={() => setWeigh(s)} title="Weigh it"><Icon name="scale" size={14} /></button>
                  <MenuButton label="Spool" align="right" items={(): MenuItem[] => [
                    { label: 'Edit', icon: 'edit', onSelect: () => setEdit(s) },
                    { label: 'Weigh it', icon: 'scale', onSelect: () => setWeigh(s) },
                    { label: s.money_ref ? 'In Pocket Money' : 'Write it down in Pocket Money', icon: 'wallet', disabled: !moneyOn || !!s.money_ref || !(s.price || s.filament_price), hint: moneyOn ? undefined : 'Needs Pocket Money', onSelect: () => toMoney(s) },
                    { label: s.archived ? 'Back on the shelf' : 'It is empty', icon: s.archived ? 'undo' : 'archive', onSelect: async () => { await api.put(`${V1}/spools/${s.id}`, { archived: !s.archived }).catch(() => {}); load(); announceChange() } },
                  ]}><Icon name="more" size={15} /></MenuButton>
                </div>
              </article>
            )
          })}
        </div>
      )}
      {edit && <SpoolDialog spool={edit === 'new' ? undefined : edit} onClose={() => { setEdit(null); load() }} />}
      {weigh && <WeighDialog spool={weigh} onClose={() => { setWeigh(null); load() }} />}
      {fil && <FilamentDialog filament={fil === 'new' ? undefined : fil} onClose={() => { setFil(null); load() }} />}
      {open && <SpoolHistory spool={open} onClose={() => setOpen(null)} onEdit={() => { setEdit(open); setOpen(null) }} />}
      {spoolman && <SpoolmanDialog onClose={() => { setSpoolman(false); load() }} />}
    </div>
  )
}

function SpoolHistory({ spool, onClose, onEdit }: { spool: Spool; onClose: () => void; onEdit: () => void }) {
  const { settings } = useApp()
  const [d, setD] = useState<{ spool: Spool; log: SpoolLog[]; prints: Print[] } | null>(null)
  useEffect(() => { api.get<typeof d>(`${V1}/spools/${spool.id}`).then(setD).catch(() => {}) }, [spool.id])
  const cur = settings?.currency || 'EUR'
  const price = spool.price || spool.filament_price
  return (
    <Modal title={`${spool.material} ${spool.name}`} onClose={onClose} footer={<><button type="button" className="btn" onClick={onEdit}><Icon name="edit" size={15} />Edit</button><span className="spacer" /><button type="button" className="btn primary" onClick={onClose}>Close</button></>}>
      <div className="stack">
        <div className="row"><Swatch color={spool.color} size={26} /><div className="grow"><b>{Math.round(spool.left_g)} g left</b> <span className="muted">of {Math.round(spool.initial_g)} g · {spool.brand}</span><div className="muted small">{[spool.bought_on && `bought ${fmtDate(spool.bought_on)}`, price && `${money(price, cur)} (${money(Math.round(price / (spool.initial_g / 1000)), cur)}/kg)`, spool.dried_on && `dried ${fmtDate(spool.dried_on)}`].filter(Boolean).join(' · ')}</div></div></div>
        <Bar value={spool.initial_g ? spool.left_g / spool.initial_g : 0} />
        <h4 className="sub-h">What happened to it</h4>
        {!d ? <Loading /> : d.log.length === 0 ? <p className="muted small">Nothing yet.</p> : <ul className="list">{d.log.slice(0, 40).map(l => (
          <li key={l.id}><div className="list-row compact">
            <Icon name={l.kind === 'print' ? 'printer' : l.kind === 'weigh' ? 'scale' : 'edit'} size={14} />
            <div className="grow min0"><div className="ellipsis small">{l.print_id ? <Link to={`/prints/${l.print_id}`}>{l.title || l.note}</Link> : (l.note || (l.kind === 'weigh' ? 'Weighed' : 'Corrected'))}</div><div className="muted small">{fmtDateTime(l.at)}</div></div>
            <span className={`tnum small ${l.grams > 0 ? '' : 'ok'}`}>{l.grams > 0 ? '−' : '+'}{Math.abs(Math.round(l.grams * 10) / 10)} g</span>
          </div></li>
        ))}</ul>}
        {d && d.prints.some(p => p.status === 'failed') && <p className="muted small">{d.prints.filter(p => p.status === 'failed').length} of the last prints with it failed.</p>}
        {d && d.prints.length > 0 && <div className="chips">{d.prints.slice(0, 6).map(p => <Link key={p.id} to={`/prints/${p.id}`} className="chip">{p.title} <PrintBadge status={p.status} /></Link>)}</div>}
      </div>
    </Modal>
  )
}

function SpoolmanDialog({ onClose }: { onClose: () => void }) {
  const { toast } = useToast()
  const [url, setUrl] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const body = file ? JSON.parse(await file.text()) : { url }
      const r = await api.post<{ text: string }>(`${V1}/import/spoolman`, body)
      toast(r.text); announceChange(); onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Bring spools over from Spoolman" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={run} disabled={busy || (!url.trim() && !file)}><Icon name="download" size={15} />{busy ? 'Bringing them…' : 'Bring them over'}</button></>}>
      <div className="stack">
        <p className="muted small">Filaments and spools with what is left on each. Doing it again updates the weights and adds only new spools.</p>
        <Field label="Spoolman’s address" help="Like http://spoolman.local:7912"><input value={url} onChange={e => setUrl(e.target.value)} placeholder="http://192.168.1.20:7912" /></Field>
        <Field label="Or its export (JSON)" help="Spoolman → Settings → Export → spools as JSON."><input type="file" accept=".json,application/json" onChange={e => setFile(e.target.files?.[0] || null)} /></Field>
      </div>
    </Modal>
  )
}
