// The history: every print, and one print with how it went, its photo,
// the settings, what it cost, and where it can go next.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { REASONS, grams, hm, money, reasonLabel, settingLabel, sortedSettings } from '../lib/farm'
import type { Job, Model, Print, Printer } from '../lib/types'
import { fmtDate, fmtDateTime } from '../lib/format'
import { Confirm, Empty, Field, Loading, Modal, Segmented, Switch, useDebounce, useToast, errMsg } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { AppButton, Bar, Cover, InfoGrid, PrintBadge, Stars, Swatch } from '../components/bits'
import { FinishDialog, RecordDialog, useMoney } from '../components/dialogs'
import { useCrumb } from '../components/Layout'

export default function Prints() {
  const [sp] = useSearchParams()
  const fmtMoney = useMoney()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [status, setStatus] = useState('')
  const [printer, setPrinter] = useState(sp.get('printer') || '')
  const [printers, setPrinters] = useState<Printer[]>([])
  const [d, setD] = useState<{ prints: Print[]; total: number } | null>(null)
  const [limit, setLimit] = useState(100)
  const [record, setRecord] = useState(false)
  const load = useCallback(() => {
    const p = new URLSearchParams({ q: dq, limit: String(limit) })
    if (status) p.set('status', status)
    if (printer) p.set('printer', printer)
    api.get<{ prints: Print[]; total: number }>(`${V1}/prints?${p}`).then(setD).catch(() => setD({ prints: [], total: 0 }))
  }, [dq, status, printer, limit])
  useEffect(load, [load])
  useOnChange(load)
  useEffect(() => { api.get<{ printers: Printer[] }>(`${V1}/printers?archived=1`).then(r => setPrinters(r.printers)).catch(() => {}) }, [])
  let lastDay = ''
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">History</h2><p className="muted">{d ? `${d.total} ${d.total === 1 ? 'print' : 'prints'}` : ' '}</p></div>
        <a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} /><span className="hide-sm">CSV</span></a>
        <button type="button" className="btn primary" onClick={() => setRecord(true)}><Icon name="plus" size={15} />Write one down</button>
      </div>
      <div className="toolbar row wrap">
        <div className="search-box grow"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a print" aria-label="Find" /></div>
        <Segmented size="sm" value={status} onChange={setStatus} options={[{ value: '', label: 'All' }, { value: 'done', label: 'Done' }, { value: 'failed', label: 'Failed' }, { value: 'printing,paused', label: 'Now' }]} />
        <Select ariaLabel="Printer" value={printer} onChange={setPrinter} options={printers.map(p => ({ value: String(p.id), label: p.name }))} placeholder="All printers" clearLabel="All printers" />
      </div>
      {!d ? <Loading /> : d.prints.length === 0 ? <Empty title="No prints" icon="history">Prints show here as they start and end.</Empty> : (
        <div className="card flush">
          <ul className="list">
            {d.prints.map(p => {
              const day = p.started_at.slice(0, 10)
              const head = day !== lastDay
              lastDay = day
              return (
                <li key={p.id}>
                  {head && <div className="day-head">{fmtDate(day)}</div>}
                  <Link to={`/prints/${p.id}`} className="list-row">
                    {p.has_photo ? <Cover src={`${V1}/prints/${p.id}/photo`} size={44} /> : <Cover src={p.has_cover && p.model_id ? `${V1}/models/${p.model_id}/cover` : false} size={44} />}
                    <div className="grow min0">
                      <div className="ellipsis"><b>{p.title}</b>{p.copies > 1 && <span className="muted"> × {p.copies}</span>}</div>
                      <div className="muted small ellipsis">{[p.printer, hm(p.seconds), p.grams ? grams(p.grams) : '', p.cost ? fmtMoney(p.cost) : '', p.status === 'failed' && reasonLabel(p.fail_reason)].filter(Boolean).join(' · ')}</div>
                    </div>
                    {p.color && <Swatch color={p.color} title={p.spool} />}
                    {p.rating > 0 && <span className="hide-sm"><Stars value={p.rating} size={12} /></span>}
                    <PrintBadge status={p.status} />
                  </Link>
                </li>
              )
            })}
          </ul>
          {d.total > d.prints.length && <div className="pad center"><button type="button" className="btn" onClick={() => setLimit(l => l + 200)}>Show more</button></div>}
        </div>
      )}
      {record && <RecordDialog onClose={() => { setRecord(false); load() }} />}
    </div>
  )
}

type PrintRes = { print: Print; model?: Model; job?: Job; price: number; currency: string }

export function PrintPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const [d, setD] = useState<PrintRes | null>(null)
  const [finish, setFinish] = useState<'done' | 'failed' | null>(null)
  const [del, setDel] = useState(false)
  const [diag, setDiag] = useState(false)
  const [setts, setSetts] = useState(false)
  const load = useCallback(() => { api.get<PrintRes>(`${V1}/prints/${id}`).then(setD).catch(e => { toast(errMsg(e), 'err'); navigate('/prints') }) }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load])
  useOnChange(load)
  useCrumb(d?.print.title, '/prints')
  if (!d) return <Loading />
  const p = d.print
  const cur = d.currency
  const ended = p.status !== 'printing' && p.status !== 'paused'
  const put = async (body: Partial<Print>) => { try { await api.put(`${V1}/prints/${p.id}`, body); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  const photo = async (f: File | undefined) => {
    if (!f) return
    try { await upload(`${V1}/prints/${p.id}/photo`, 'file', f); toast('Photo kept'); load() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => { try { await api.del(`${V1}/prints/${p.id}`); announceChange(); navigate('/prints') } catch (e) { toast(errMsg(e), 'err') } }
  const set = sortedSettings(p.settings)
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow min0">
          <h2 className="page-title row">{p.title}<PrintBadge status={p.status} /></h2>
          <p className="muted">{[fmtDateTime(p.started_at), p.printer, p.copies > 1 ? `${p.copies} pieces` : ''].filter(Boolean).join(' · ')}{p.model_id && <> · <Link to={`/models/${p.model_id}`}>the model</Link></>}</p>
        </div>
        <div className="row wrap">
          {!ended && <><button type="button" className="btn" onClick={() => setFinish('failed')}><Icon name="x-circle" size={15} />It failed</button><button type="button" className="btn primary" onClick={() => setFinish('done')}><Icon name="check" size={15} />It is done</button></>}
          <button type="button" className="btn ghost icon-only" aria-label="Remove" title="Remove" onClick={() => setDel(true)}><Icon name="trash" size={15} /></button>
        </div>
      </div>
      <div className="cols2">
        <div className="stack">
          <section className="card flush photo-card">
            {p.has_photo ? <img src={`${V1}/prints/${p.id}/photo?v=${encodeURIComponent(p.created_at)}`} alt="The print" className="print-photo" /> : <div className="photo-empty"><Cover src={p.has_cover && p.model_id ? `${V1}/models/${p.model_id}/cover` : false} size={140} /><span className="muted small">No photo of it yet</span></div>}
            <div className="photo-tools">
              <label className="btn sm"><Icon name="camera" size={14} />{p.has_photo ? 'Another photo' : 'Add a photo'}<input type="file" accept="image/*" capture="environment" hidden onChange={e => photo(e.target.files?.[0])} /></label>
              {p.has_photo && <AppButton slug="files" scope="files:write" label="To Files" icon="archive" path={`${V1}/prints/${p.id}/files`} />}
            </div>
          </section>
          {!ended ? <section className="card pad-card"><Bar value={p.progress} /><p className="muted small mt-s">{Math.round(p.progress * 100)}% · printing for {hm(Math.max(p.seconds, (Date.now() - new Date(p.started_at).getTime()) / 1000))}{p.est_seconds ? ` of about ${hm(p.est_seconds)}` : ''}</p></section> : (
            <section className="card">
              <div className="card-head"><h3>How it came out</h3></div>
              <div className="pad stack">
                {p.status === 'done' ? <div className="row"><Stars value={p.rating} onChange={v => put({ rating: v })} size={24} /><span className="muted small">{p.rating ? '' : 'Rate it'}</span></div> : <>
                  <Field label="What happened"><Select ariaLabel="Reason" value={p.fail_reason} onChange={v => put({ fail_reason: v })} options={REASONS.map(r => ({ value: r.value, label: r.label }))} /></Field>
                  {p.fail_note && <p className="muted">“{p.fail_note}”{p.progress > 0 && ` — at ${Math.round(p.progress * 100)}%`}</p>}
                  <button type="button" className="btn" disabled={!settings?.ai_help} onClick={() => setDiag(true)}><Icon name="sparkles" size={15} />What went wrong? (AI)</button>
                </>}
                <NotesBox value={p.notes} onSave={v => put({ notes: v })} />
              </div>
            </section>
          )}
          <section className="card">
            <div className="card-head"><h3>Settings</h3><span className="spacer" /><label className="check-row small" title="Show these on the model as settings that worked"><Switch size="sm" checked={p.keep} onChange={v => put({ keep: v })} label="These worked" />These worked</label><button type="button" className="btn ghost sm" onClick={() => setSetts(true)}><Icon name="edit" size={13} /></button></div>
            <div className="pad">{set.length ? <InfoGrid items={set.map(([k, v]) => [settingLabel(k), v])} /> : <span className="muted small">None written down. Add them, or slice the model and add the G-code: its settings come with it.</span>}</div>
          </section>
        </div>
        <div className="stack">
          <section className="card">
            <div className="card-head"><h3>The numbers</h3></div>
            <div className="pad">
              <InfoGrid items={[
                ['Printer', p.printer], ['Filament', p.spool ? <span className="row"><Swatch color={p.color} />{p.spool}</span> : ''], ['Time', `${hm(p.seconds)}${p.est_seconds ? ` (the slicer said ${hm(p.est_seconds)})` : ''}`],
                ['Used', p.grams ? `${grams(p.grams)}${p.est_grams ? ` (said ${grams(p.est_grams)})` : ''}` : ''], ['File', p.file_name], ['Ended', p.ended_at ? fmtDateTime(p.ended_at) : ''],
              ]} />
            </div>
          </section>
          {ended && <section className="card">
            <div className="card-head"><h3>What it cost</h3></div>
            <div className="pad">
              <div className="cost-rows">
                <div><span>Filament</span><b className="tnum">{money(p.filament_cost, cur)}</b></div>
                <div><span>Electricity</span><b className="tnum">{money(p.power_cost, cur)}</b></div>
                <div><span>Wear</span><b className="tnum">{money(p.wear_cost, cur)}</b></div>
                <div className="total"><span>All</span><b className="tnum">{money(p.cost, cur)}</b></div>
              </div>
              {p.status === 'done' && p.cost > 0 && <p className="muted small mt">To sell one: about <b>{money(d.price, cur)}</b> (the cost{p.copies > 1 ? ' of one piece' : ''} + {settings?.markup ?? 100}%).</p>}
            </div>
          </section>}
          <section className="card">
            <div className="card-head"><h3>Send it on</h3></div>
            <div className="pad row wrap">
              {p.status === 'done' && (p.inventory_ref ? <a className="btn sm" href={p.inventory_ref} target="_blank" rel="noreferrer"><Icon name="package" size={14} />On the inventory</a>
                : <AppButton slug="inventory" scope="items:write" label="To Inventory" icon="package" path={`${V1}/prints/${p.id}/inventory`} body={{ quantity: p.copies }} onDone={load} />)}
              <AppButton slug="chat" scope="chat:write" label="Share in Chat" icon="message" path={`${V1}/prints/${p.id}/chat`} />
              <AppButton slug="projects" scope="projects:write" label="An issue in Projects" icon="layers" path={`${V1}/prints/${p.id}/issue`} />
              {!ended && (p.calendar_ref ? <a className="btn sm" href={p.calendar_ref} target="_blank" rel="noreferrer"><Icon name="calendar" size={14} />In the calendar</a>
                : <AppButton slug="calendar" scope="calendar:write" label="Its end in Calendar" icon="calendar" path={`${V1}/prints/${p.id}/calendar`} onDone={load} />)}
            </div>
          </section>
          {d.job && <section className="card pad-card"><div className="muted small">Part of the job</div><Link to="/queue"><b>{d.job.title}</b></Link> <span className="muted">· {d.job.done_count} of {d.job.quantity}{d.job.for_whom ? ` · for ${d.job.for_whom}` : ''}</span></section>}
        </div>
      </div>
      {finish && <FinishDialog print={p} initial={finish} onClose={() => setFinish(null)} onDone={load} />}
      {del && <Confirm title="Remove this print?" message="Its filament goes back on the spool." confirmLabel="Remove" onCancel={() => setDel(false)} onConfirm={remove} />}
      {diag && <Diagnose print={p} onClose={() => setDiag(false)} />}
      {setts && <SettingsEdit print={p} onClose={() => { setSetts(false); load() }} />}
    </div>
  )
}

function NotesBox({ value, onSave }: { value: string; onSave: (v: string) => void }) {
  const [v, setV] = useState(value)
  useEffect(() => setV(value), [value])
  return <textarea rows={2} value={v} onChange={e => setV(e.target.value)} onBlur={() => v !== value && onSave(v)} placeholder="Notes: what to do differently next time…" aria-label="Notes" />
}

function SettingsEdit({ print, onClose }: { print: Print; onClose: () => void }) {
  const { toast } = useToast()
  const keys = ['layer_height', 'walls', 'infill', 'infill_type', 'supports', 'brim', 'nozzle_temp', 'bed_temp', 'speed']
  const [s, setS] = useState<Record<string, string>>({ ...print.settings })
  const save = async () => { try { await api.put(`${V1}/prints/${print.id}`, { settings: s }); toast('Saved'); onClose() } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title="The settings" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save}>Save</button></>}>
      <div className="grid two">{keys.map(k => <Field key={k} label={settingLabel(k)}><input value={s[k] || ''} onChange={e => setS({ ...s, [k]: e.target.value })} /></Field>)}</div>
    </Modal>
  )
}

function Diagnose({ print, onClose }: { print: Print; onClose: () => void }) {
  const { toast } = useToast()
  const [res, setRes] = useState<{ summary: string; causes: { cause: string; check: string; change: string }[]; next_try: string[] } | null>(null)
  const [busy, setBusy] = useState(true)
  useEffect(() => {
    api.post<typeof res>(`${V1}/prints/${print.id}/diagnose`, {}).then(setRes).catch(e => { toast(errMsg(e), 'err'); onClose() }).finally(() => setBusy(false))
  }, [print.id]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal title="What probably went wrong" onClose={onClose} footer={<button type="button" className="btn primary" onClick={onClose}>Close</button>}>
      {busy || !res ? <Loading label="Pocket AI is looking at the print, the printer and the filament…" /> : (
        <div className="stack ai-box">
          <p><b>{res.summary}</b></p>
          {res.causes.map((c, i) => <div key={i} className="cause"><b>{i + 1}. {c.cause}</b><p className="small"><span className="muted">Check:</span> {c.check}</p><p className="small"><span className="muted">Next time:</span> {c.change}</p></div>)}
          {res.next_try?.length > 0 && <><h4 className="sub-h">For the next try</h4><ul className="small">{res.next_try.map((t, i) => <li key={i}>{t}</li>)}</ul></>}
          <p className="muted small">A guess from what was written down — check it against the print itself.</p>
        </div>
      )}
    </Modal>
  )
}
