// The printers: the list, and one printer with its upkeep, log book and prints.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useAppState, useOnChange } from '../lib/data'
import { CONNECTIONS, hm, money, sizeOf } from '../lib/farm'
import type { Print, Printer, PrinterView, Spool, Upkeep, UpkeepLog } from '../lib/types'
import { fmtDate, todayISO } from '../lib/format'
import { Confirm, Empty, Field, Loading, Modal, useToast, errMsg } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { InfoGrid, PrintBadge, StateBadge, Swatch } from '../components/bits'
import { MoneyInput, PrinterDialog, StartDialog } from '../components/dialogs'
import { PrinterCard, usePoll } from './Farm'
import { useCrumb } from '../components/Layout'

export default function Printers() {
  const navigate = useNavigate()
  const [list, setList] = useState<PrinterView[] | null>(null)
  const [archived, setArchived] = useState<Printer[]>([])
  const [adding, setAdding] = useState(false)
  const load = useCallback(() => {
    api.get<{ printers: PrinterView[] }>(`${V1}/farm`).then(r => setList(r.printers)).catch(() => setList([]))
    api.get<{ printers: Printer[] }>(`${V1}/printers?archived=1`).then(r => setArchived(r.printers.filter(p => p.archived))).catch(() => {})
  }, [])
  useEffect(load, [load])
  useOnChange(load)
  usePoll(load, 10000)
  const move = async (i: number, d: number) => {
    if (!list) return
    const ids = list.map(p => p.id)
    const j = i + d
    if (j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j], ids[i]]
    await api.put(`${V1}/printers/order`, { ids }).catch(() => {})
    load()
  }
  if (!list) return <Loading />
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">Printers</h2><p className="muted">{list.length} on the farm · {Math.round(list.reduce((a, p) => a + p.hours, 0))} print hours in all</p></div>
        <button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />Add a printer</button>
      </div>
      {list.length === 0 ? <Empty title="No printers yet" icon="printer">Add the first one.</Empty> : (
        <div className="card flush">
          <ul className="list">
            {list.map((p, i) => (
              <li key={p.id}>
                <div className="list-row">
                  <span className="list-ico" style={{ background: `color-mix(in srgb, ${p.color || 'var(--accent)'} 18%, transparent)`, color: p.color || 'var(--accent)' }}><Icon name="printer" size={17} /></span>
                  <Link to={`/printers/${p.id}`} className="grow min0">
                    <div className="ellipsis"><b>{p.name}</b> <span className="muted">{p.model}</span></div>
                    <div className="muted small ellipsis">{[CONNECTIONS.find(c => c.value === p.connection)?.label, p.location, `${Math.round(p.hours)} h`, p.prints ? `${Math.round((1 - p.failed / p.prints) * 100)}% good` : ''].filter(Boolean).join(' · ')}</div>
                  </Link>
                  <StateBadge state={p.state} />
                  <div className="row hide-sm">
                    <button type="button" className="btn ghost icon-only sm" aria-label="Up" disabled={i === 0} onClick={() => move(i, -1)}><Icon name="chevron-up" size={14} /></button>
                    <button type="button" className="btn ghost icon-only sm" aria-label="Down" disabled={i === list.length - 1} onClick={() => move(i, 1)}><Icon name="chevron-down" size={14} /></button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
      {archived.length > 0 && <section>
        <h3 className="sec-h">Put away</h3>
        <div className="chips">{archived.map(p => <Link key={p.id} to={`/printers/${p.id}`} className="chip">{p.name}</Link>)}</div>
      </section>}
      {adding && <PrinterDialog onClose={() => setAdding(false)} onSaved={p => navigate(`/printers/${p.id}`)} />}
    </div>
  )
}

type PrinterRes = { printer: PrinterView; upkeep: Upkeep[]; log: UpkeepLog[]; prints: Print[]; month: { hours: number; done: number; failed: number; busy: number } }

export function PrinterPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const [d, setD] = useState<PrinterRes | null>(null)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  const [start, setStart] = useState(false)
  const [spools, setSpools] = useState<Spool[]>([])
  const [upkeepEdit, setUpkeepEdit] = useState<Upkeep | 'new' | null>(null)
  const [doneFor, setDoneFor] = useState<Upkeep | 'repair' | null>(null)
  const load = useCallback(() => {
    api.get<PrinterRes>(`${V1}/printers/${id}`).then(setD).catch(e => { toast(errMsg(e), 'err'); navigate('/printers') })
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load])
  useEffect(() => { api.get<{ spools: Spool[] }>(`${V1}/spools`).then(r => setSpools(r.spools)).catch(() => {}) }, [])
  useOnChange(load)
  usePoll(load, 5000)
  useCrumb(d?.printer.name, '/printers')
  if (!d) return <Loading />
  const p = d.printer
  const cur = settings?.currency || 'EUR'
  const load2 = async (sid: string) => {
    try { await api.put(`${V1}/printers/${p.id}/spool`, { spool_id: sid ? Number(sid) : null }); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => { try { await api.del(`${V1}/printers/${p.id}`); announceChange(); navigate('/printers') } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow min0">
          <h2 className="page-title row"><Swatch color={p.color} size={14} />{p.name}</h2>
          <p className="muted">{[p.model, CONNECTIONS.find(c => c.value === p.connection)?.label, p.location].filter(Boolean).join(' · ')}</p>
        </div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={() => setEdit(true)}><Icon name="edit" size={15} />Edit</button>
          <button type="button" className="btn ghost icon-only" aria-label="Remove" title="Remove" onClick={() => setDel(true)}><Icon name="trash" size={15} /></button>
        </div>
      </div>
      <div className="cols2">
        <div className="stack">
          <PrinterCard p={p} big onStart={() => setStart(true)} onChanged={load} />
          <section className="card">
            <div className="card-head"><h3>Filament in it</h3></div>
            <div className="pad">
              <Select ariaLabel="Spool" value={p.spool_id ? String(p.spool_id) : ''} onChange={load2}
                options={spools.map(s => ({ value: String(s.id), label: `${s.material} ${s.name}`.trim(), hint: `${Math.round(s.left_g)} g${s.printer && s.printer_id !== p.id ? ' · in ' + s.printer : ''}`, group: s.material }))} placeholder="No spool loaded" clearLabel="Take it out" />
            </div>
          </section>
          <section className="card">
            <div className="card-head"><h3>The last 30 days</h3></div>
            <div className="stats-row tight pad">
              <div className="stat"><div className="stat-l">Printing</div><div className="stat-v tnum">{d.month.hours} h</div><div className="stat-s small">{d.month.busy}% of the time</div></div>
              <div className="stat"><div className="stat-l">Done</div><div className="stat-v tnum">{d.month.done}</div></div>
              <div className="stat"><div className="stat-l">Failed</div><div className="stat-v tnum">{d.month.failed}</div></div>
            </div>
          </section>
          <section className="card">
            <div className="card-head"><h3>About it</h3></div>
            <div className="pad">
              <InfoGrid items={[
                ['Print hours', `${Math.round(p.hours)} h`], ['Prints', `${p.prints}${p.prints ? ` (${Math.round((1 - p.failed / p.prints) * 100)}% good)` : ''}`],
                ['Bed', sizeOf(p.bed_x, p.bed_y, p.bed_z)], ['Nozzle', p.nozzle ? `${p.nozzle} mm` : ''], ['Prints in', p.materials],
                ['Power', p.watts ? `${p.watts} W` : ''], ['Wear', p.wear_per_hour ? `${money(p.wear_per_hour, cur)} an hour` : ''],
                ['Bought', p.bought_on ? fmtDate(p.bought_on) : ''], ['Price', p.price ? money(p.price, cur) : ''],
                ['Address', p.url], ['Notes', p.notes],
              ]} />
            </div>
          </section>
        </div>
        <div className="stack">
          <section className="card">
            <div className="card-head"><h3>Upkeep</h3><span className="spacer" /><button type="button" className="btn sm" onClick={() => setUpkeepEdit('new')}><Icon name="plus" size={13} />Add</button></div>
            {d.upkeep.length === 0 ? <div className="pad muted small">Nothing yet. Add what this printer needs every so many print hours or days: cleaning the nozzle, greasing the rods, the belts.</div> : (
              <ul className="list">{d.upkeep.map(u => <UpkeepRow key={u.id} u={u} onDone={() => setDoneFor(u)} onEdit={() => setUpkeepEdit(u)} />)}</ul>
            )}
          </section>
          <section className="card">
            <div className="card-head"><h3>Log book</h3><span className="spacer" /><button type="button" className="btn sm" onClick={() => setDoneFor('repair')}><Icon name="wrench" size={13} />A repair</button></div>
            {d.log.length === 0 ? <div className="pad muted small">Upkeep done and repairs show here.</div> : (
              <ul className="list">{d.log.slice(0, 12).map(l => (
                <li key={l.id}><div className="list-row compact">
                  <div className="grow min0"><div className="ellipsis">{l.title}</div><div className="muted small">{fmtDate(l.done_on)} · {Math.round(l.hours)} h{l.cost ? ` · ${money(l.cost, cur)}` : ''}{l.note ? ` · ${l.note}` : ''}</div></div>
                </div></li>
              ))}</ul>
            )}
          </section>
          <section className="card">
            <div className="card-head"><h3>Prints</h3><span className="spacer" /><Link to={`/prints?printer=${p.id}`} className="btn ghost sm">All<Icon name="chevron-right" size={14} /></Link></div>
            {d.prints.length === 0 ? <div className="pad muted small">No prints yet.</div> : <ul className="list">{d.prints.slice(0, 10).map(x => (
              <li key={x.id}><Link to={`/prints/${x.id}`} className="list-row compact">
                <div className="grow min0"><div className="ellipsis">{x.title}</div><div className="muted small">{fmtDate(x.started_at.slice(0, 10))} · {hm(x.seconds)}{x.grams ? ` · ${Math.round(x.grams)} g` : ''}</div></div>
                <PrintBadge status={x.status} />
              </Link></li>
            ))}</ul>}
          </section>
        </div>
      </div>
      {edit && <PrinterDialog printer={p} onClose={() => setEdit(false)} onSaved={load} />}
      {del && <Confirm title={`Remove ${p.name}?`} message="Its prints stay in the history, without the printer. To keep it, put it away instead (Edit → Put away)." confirmLabel="Remove" onCancel={() => setDel(false)} onConfirm={remove} />}
      {start && <StartDialog printer={p} job={p.next || undefined} onClose={() => setStart(false)} onStarted={load} />}
      {upkeepEdit && <UpkeepDialog printerId={p.id} item={upkeepEdit === 'new' ? undefined : upkeepEdit} onClose={() => { setUpkeepEdit(null); load() }} />}
      {doneFor && <UpkeepDoneDialog printer={p} item={doneFor === 'repair' ? undefined : doneFor} onClose={() => { setDoneFor(null); load() }} />}
    </div>
  )
}

export function UpkeepRow({ u, onDone, onEdit, showPrinter = false }: { u: Upkeep; onDone: () => void; onEdit: () => void; showPrinter?: boolean }) {
  const when = [u.every_hours ? `every ${u.every_hours} h` : '', u.every_days ? `every ${u.every_days} days` : ''].filter(Boolean).join(' or ')
  const left = [u.due_hours !== null ? (u.due_hours <= 0 ? `${Math.round(-u.due_hours)} h over` : `${Math.round(u.due_hours)} h to go`) : '', u.due_on ? (u.due ? `since ${fmtDate(u.due_on)}` : `by ${fmtDate(u.due_on)}`) : ''].filter(Boolean).join(' · ')
  return (
    <li><div className="list-row compact">
      <span className={`up-dot ${u.due ? 'due' : u.soon ? 'soon' : ''}`} />
      <button type="button" className="grow min0 linkish plain" onClick={onEdit}>
        <div className="ellipsis">{showPrinter && <b>{u.printer}: </b>}{u.title}</div>
        <div className="muted small ellipsis">{when}{left && ` · ${left}`}</div>
      </button>
      <button type="button" className={`btn sm ${u.due ? 'primary' : ''}`} onClick={onDone}><Icon name="check" size={13} />Done</button>
    </div></li>
  )
}

export function UpkeepDialog({ printerId, item, onClose }: { printerId?: number; item?: Upkeep; onClose: () => void }) {
  const { toast } = useToast()
  const [printers, setPrinters] = useState<Printer[]>([])
  const [f, setF] = useState({ printer_id: item?.printer_id || printerId || 0, title: item?.title || '', every_hours: item?.every_hours || 0, every_days: item?.every_days || 0, notes: item?.notes || '' })
  useEffect(() => { if (!printerId) api.get<{ printers: Printer[] }>(`${V1}/printers`).then(r => setPrinters(r.printers)).catch(() => {}) }, [printerId])
  const save = async () => {
    try { item ? await api.put(`${V1}/upkeep/${item.id}`, f) : await api.post(`${V1}/upkeep`, f); toast('Saved'); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => { if (!item) return; try { await api.del(`${V1}/upkeep/${item.id}`); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') } }
  const presets = [['Clean the nozzle and the bed', 50, 0], ['Lubricate the rods / rails', 200, 0], ['Check the belt tension', 0, 30], ['Replace the nozzle', 400, 0], ['Replace the PTFE tube', 500, 0], ['Tighten the frame screws', 0, 90], ['Clean the fans', 0, 60]] as const
  return (
    <Modal title={item ? 'Edit the upkeep' : 'Upkeep for the printer'} onClose={onClose} size="sm" footer={<>
      {item && <button type="button" className="btn danger ghost" onClick={remove}><Icon name="trash" size={15} />Remove</button>}
      <span className="spacer" />
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={!f.title.trim() || !f.printer_id}>Save</button>
    </>}>
      <div className="stack">
        {!printerId && <Field label="Printer"><Select ariaLabel="Printer" value={f.printer_id ? String(f.printer_id) : ''} onChange={v => setF({ ...f, printer_id: Number(v) })} options={printers.map(p => ({ value: String(p.id), label: p.name }))} /></Field>}
        <Field label="What needs doing"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="Clean the nozzle" /></Field>
        {!item && <div className="chips">{presets.map(([t, h, dd]) => <button key={t} type="button" className="chip" onClick={() => setF({ ...f, title: t, every_hours: h, every_days: dd })}>{t}</button>)}</div>}
        <div className="grid two">
          <Field label="Every so many print hours"><div className="input-suffix"><input type="number" min={0} value={f.every_hours || ''} onChange={e => setF({ ...f, every_hours: Number(e.target.value) })} /><span>h</span></div></Field>
          <Field label="Or every so many days"><div className="input-suffix"><input type="number" min={0} value={f.every_days || ''} onChange={e => setF({ ...f, every_days: Number(e.target.value) })} /><span>days</span></div></Field>
        </div>
        <Field label="How (notes)"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

export function UpkeepDoneDialog({ printer, item, onClose }: { printer: Printer; item?: Upkeep; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const state = useAppState()
  const homeOn = state('home', 'home:write').ok && !!printer.home_ref
  const [f, setF] = useState({ title: item?.title || '', done_on: todayISO(), cost: 0, note: '', to_home: homeOn })
  const save = async () => {
    try {
      const r = await api.post<{ home_url?: string; home_error?: string }>(`${V1}/upkeep/done`, { upkeep_id: item?.id || 0, printer_id: printer.id, ...f })
      toast(r.home_url ? 'Written down here and in Pocket Home' : 'Written down'); if (r.home_error) toast(r.home_error, 'err'); announceChange(); onClose()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title={item ? `Done: ${item.title}` : 'A repair'} onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={!item && !f.title.trim()}>Save</button>
    </>}>
      <div className="stack">
        {!item && <Field label="What was done"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="New hotend" /></Field>}
        <div className="grid two">
          <Field label="When"><input type="date" value={f.done_on} onChange={e => setF({ ...f, done_on: e.target.value })} /></Field>
          <Field label="It cost"><MoneyInput value={f.cost} onChange={v => setF({ ...f, cost: v })} currency={settings?.currency} /></Field>
        </div>
        <Field label="Note"><input value={f.note} onChange={e => setF({ ...f, note: e.target.value })} /></Field>
        <label className="check-row"><input type="checkbox" checked={f.to_home} disabled={!homeOn} onChange={e => setF({ ...f, to_home: e.target.checked })} />
          <span>Also in Pocket Home’s log for the appliance{!printer.home_ref && <span className="muted small"> (link the printer to it first: Edit)</span>}</span></label>
      </div>
    </Modal>
  )
}
