// The forms that open over a page.
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, uploadFiles, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useAppState } from '../lib/data'
import { CONNECTIONS, MATERIALS, REASONS, hm, kindLabel, money } from '../lib/farm'
import type { Filament, Job, Model, ModelFile, Print, Printer, PrinterView, Spool } from '../lib/types'
import { fmtBytes, todayISO } from '../lib/format'
import { Field, Modal, Segmented, Spinner, Switch, Tabs, useToast, errMsg, CoreHint } from './ui'
import { Select, type SelectOption } from './Select'
import { Icon } from './icons'

function useList<T>(path: string, key: string, on = true): T[] | null {
  const [v, setV] = useState<T[] | null>(null)
  useEffect(() => { if (on) api.get<Record<string, T[]>>(`${V1}${path}`).then(r => setV(r[key] || [])).catch(() => setV([])) }, [path, key, on])
  return v
}

/** Money typed in major units ("12.50"), kept in minor units (1250). */
export function MoneyInput({ value, onChange, placeholder = '0.00', currency }: { value: number; onChange: (v: number) => void; placeholder?: string; currency?: string }) {
  const [text, setText] = useState(value ? (value / 100).toFixed(2) : '')
  useEffect(() => { const cur = Math.round(parseFloat(text.replace(',', '.')) * 100) || 0; if (cur !== value) setText(value ? (value / 100).toFixed(2) : '') }, [value]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <div className="input-suffix">
      <input inputMode="decimal" value={text} placeholder={placeholder} onChange={e => { setText(e.target.value); onChange(Math.round(parseFloat(e.target.value.replace(',', '.')) * 100) || 0) }} />
      {currency && <span>{currency}</span>}
    </div>
  )
}

function NumberInput({ value, onChange, step = 1, min = 0, suffix }: { value: number; onChange: (v: number) => void; step?: number; min?: number; suffix?: string }) {
  return (
    <div className="input-suffix">
      <input type="number" inputMode="decimal" min={min} step={step} value={Number.isFinite(value) ? value : ''} onChange={e => onChange(e.target.value === '' ? 0 : Number(e.target.value))} />
      {suffix && <span>{suffix}</span>}
    </div>
  )
}

const spoolLabel = (s: Spool) => `${s.material} ${s.name || ''} ${s.brand ? '— ' + s.brand : ''}`.replace(/\s+/g, ' ').trim()

function SpoolSelect({ value, onChange, spools, placeholder = 'No spool' }: { value: number | null; onChange: (v: number | null) => void; spools: Spool[]; placeholder?: string }) {
  const opts: SelectOption[] = spools.filter(s => !s.archived).map(s => ({ value: String(s.id), label: spoolLabel(s), hint: `${Math.round(s.left_g)} g left${s.printer ? ' · in ' + s.printer : ''}`, group: s.material }))
  return <Select ariaLabel="Spool" value={value ? String(value) : ''} onChange={v => onChange(v ? Number(v) : null)} options={opts} placeholder={placeholder} clearLabel={placeholder} />
}

// ---------- a job in the queue ----------

export function JobDialog({ job, modelId, onClose, onSaved }: { job?: Job; modelId?: number; onClose: () => void; onSaved?: (j: Job) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const models = useList<Model>('/models?limit=1000&sort=name', 'models')
  const printers = useList<Printer>('/printers', 'printers')
  const filaments = useList<Filament>('/filaments', 'filaments')
  const state = useAppState()
  const contactsOn = state('contacts', 'contacts:read').ok
  const [f, setF] = useState({
    title: job?.title || '', model_id: job?.model_id || modelId || 0, file_id: job?.file_id || 0, printer_id: job?.printer_id || 0, filament_id: job?.filament_id || 0,
    material: job?.material || '', quantity: job?.quantity || 1, due_on: job?.due_on || '', for_whom: job?.for_whom || '', price: job?.price || 0, notes: job?.notes || '',
  })
  const [files, setFiles] = useState<ModelFile[]>([])
  const [busy, setBusy] = useState(false)
  const [people, setPeople] = useState<{ id: string; name: string }[] | null>(null)
  useEffect(() => {
    if (!f.model_id) { setFiles([]); return }
    api.get<{ files: ModelFile[] }>(`${V1}/models/${f.model_id}`).then(r => {
      setFiles(r.files)
      const g = r.files.filter(x => x.printable)
      setF(x => x.file_id && r.files.some(y => y.id === x.file_id) ? x : { ...x, file_id: g.length ? g[g.length - 1].id : 0, material: x.material || g[g.length - 1]?.material || '' })
    }).catch(() => setFiles([]))
  }, [f.model_id])
  const gcode = files.filter(x => x.printable)
  const save = async () => {
    setBusy(true)
    try {
      const body = { ...f, title: f.title || models?.find(m => m.id === f.model_id)?.name || '' }
      const j = job ? await api.put<Job>(`${V1}/jobs/${job.id}`, body) : await api.post<Job>(`${V1}/jobs`, body)
      toast(job ? 'Saved' : 'In the queue'); announceChange(); onSaved?.(j); onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const findPeople = async () => {
    try { setPeople((await api.get<{ contacts: { id: string; name: string }[] }>(`${V1}/apps/contacts?q=${encodeURIComponent(f.for_whom)}`)).contacts) } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title={job ? 'Edit the job' : 'Add to the queue'} onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={busy || (!f.model_id && !f.title.trim())}>{job ? 'Save' : 'Add to the queue'}</button>
    </>}>
      <div className="stack">
        <Field label="Model">
          <Select ariaLabel="Model" value={f.model_id ? String(f.model_id) : ''} onChange={v => setF({ ...f, model_id: Number(v) || 0, file_id: 0 })}
            options={(models || []).map(m => ({ value: String(m.id), label: m.name, hint: m.tags }))} placeholder={models ? 'Choose from the library' : 'Loading…'} clearLabel="No model (just a name)" />
        </Field>
        {!!f.model_id && <Field label="G-code" help={gcode.length ? 'The file sent to a connected printer.' : 'This model has no sliced G-code yet: it can still be printed by hand, or started on the printer.'}>
          <Select ariaLabel="G-code" value={f.file_id ? String(f.file_id) : ''} onChange={v => setF({ ...f, file_id: Number(v) || 0 })}
            options={gcode.map(x => ({ value: String(x.id), label: x.name, hint: [hm(x.est_seconds), x.est_grams ? `${Math.round(x.est_grams)} g` : '', x.material].filter(Boolean).join(' · ') }))} placeholder="No G-code" clearLabel="No G-code" />
        </Field>}
        <Field label="Name" help={f.model_id ? 'Leave empty to use the model’s name.' : undefined}><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder={models?.find(m => m.id === f.model_id)?.name || 'Spare knob for the kitchen'} /></Field>
        <div className="grid two">
          <Field label="How many"><NumberInput value={f.quantity} onChange={v => setF({ ...f, quantity: Math.max(1, Math.round(v)) })} min={1} /></Field>
          <Field label="By"><input type="date" value={f.due_on} onChange={e => setF({ ...f, due_on: e.target.value })} /></Field>
        </div>
        <div className="grid two">
          <Field label="Printer">
            <Select ariaLabel="Printer" value={f.printer_id ? String(f.printer_id) : ''} onChange={v => setF({ ...f, printer_id: Number(v) || 0 })}
              options={(printers || []).map(p => ({ value: String(p.id), label: p.name, hint: p.model }))} placeholder="The first one free" clearLabel="The first one free" />
          </Field>
          <Field label="Filament">
            <Select ariaLabel="Filament" value={f.filament_id ? String(f.filament_id) : f.material ? 'm:' + f.material : ''}
              onChange={v => v.startsWith('m:') ? setF({ ...f, filament_id: 0, material: v.slice(2) }) : setF({ ...f, filament_id: Number(v) || 0, material: filaments?.find(x => x.id === Number(v))?.material || '' })}
              options={[...MATERIALS.map(m => ({ value: 'm:' + m, label: `Any ${m}`, group: 'Any colour' })), ...(filaments || []).map(x => ({ value: String(x.id), label: `${x.material} ${x.name}`.trim(), hint: x.brand, group: 'This one' }))]}
              placeholder={settings?.default_material ? `Any (${settings.default_material} usually)` : 'Any'} clearLabel="Any" />
          </Field>
        </div>
        <Field label="For whom" help={contactsOn ? undefined : 'Pocket Contacts can fill this in when it is connected.'}>
          <div className="row">
            <input value={f.for_whom} onChange={e => setF({ ...f, for_whom: e.target.value })} placeholder="Ana, the market stall…" />
            <button type="button" className="btn" disabled={!contactsOn} onClick={findPeople} title={contactsOn ? 'Find in Pocket Contacts' : 'Needs Pocket Contacts'}><Icon name="users" size={15} /></button>
          </div>
          {people && <div className="chips mt-s">{people.length ? people.slice(0, 12).map(p => <button key={p.id} type="button" className="chip" onClick={() => { setF({ ...f, for_whom: p.name }); setPeople(null) }}>{p.name}</button>) : <span className="muted small">Nobody by that name.</span>}</div>}
        </Field>
        <Field label="Sold for (each)" help="Leave at 0 when it is not sold. With a price, the job can become an invoice in Pocket Invoicing."><MoneyInput value={f.price} onChange={v => setF({ ...f, price: v })} currency={settings?.currency} /></Field>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} placeholder="Colours, what it is for…" /></Field>
      </div>
    </Modal>
  )
}

// ---------- start a print ----------

export function StartDialog({ printer, job, model, onClose, onStarted }: { printer?: PrinterView | Printer; job?: Job; model?: Model; onClose: () => void; onStarted?: (p: Print) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const printers = useList<PrinterView>('/farm', 'printers')
  const spools = useList<Spool>('/spools', 'spools')
  const jobs = useList<Job>('/jobs', 'jobs', !job && !model)
  const [printerId, setPrinterId] = useState<number>(printer?.id || job?.printer_id || 0)
  const [jobId, setJobId] = useState<number>(job?.id || 0)
  const [spoolId, setSpoolId] = useState<number | null>(null)
  const [copies, setCopies] = useState(1)
  const [send, setSend] = useState(settings?.send_gcode ?? true)
  const [busy, setBusy] = useState(false)
  const p = printers?.find(x => x.id === printerId)
  useEffect(() => { if (p && spoolId === null) setSpoolId(p.spool_id) }, [p]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!printerId && printers?.length) {
      const free = printers.find(x => !x.print && !x.needs_clear && x.state !== 'offline' && x.state !== 'error')
      if (free) setPrinterId(free.id)
    }
  }, [printers]) // eslint-disable-line react-hooks/exhaustive-deps
  const connected = p && p.connection !== 'manual'
  const sel = job || jobs?.find(j => j.id === jobId)
  const start = async () => {
    setBusy(true)
    try {
      const body = { printer_id: printerId, job_id: sel?.id || 0, model_id: model?.id || 0, spool_id: spoolId && spoolId !== p?.spool_id ? spoolId : 0, copies, send, force: true }
      const pr = sel ? await api.post<Print>(`${V1}/jobs/${sel.id}/start`, body) : await api.post<Print>(`${V1}/prints`, body)
      toast(connected && send ? `Sent to ${p?.name}: it starts printing` : `${p?.name} is printing`); announceChange(); onStarted?.(pr); onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Start a print" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={start} disabled={busy || !printerId || (!sel && !model)}>{busy ? <Spinner /> : <Icon name="play" size={15} />}Start</button>
    </>}>
      <div className="stack">
        <Field label="Printer">
          <Select ariaLabel="Printer" value={printerId ? String(printerId) : ''} onChange={v => { setPrinterId(Number(v) || 0); setSpoolId(null) }}
            options={(printers || []).map(x => ({ value: String(x.id), label: x.name, hint: x.print ? `printing ${x.print.title}` : x.needs_clear ? 'the bed is not clear' : x.state === 'offline' ? 'not answering' : x.model, disabled: !!x.print }))} placeholder="Choose a printer" />
        </Field>
        {!job && !model && <Field label="What">
          <Select ariaLabel="Job" value={jobId ? String(jobId) : ''} onChange={v => setJobId(Number(v) || 0)}
            options={(jobs || []).filter(j => j.status === 'queued').map(j => ({ value: String(j.id), label: j.title, hint: `${j.done_count}/${j.quantity}${j.for_whom ? ' · ' + j.for_whom : ''}` }))} placeholder="A job from the queue" />
        </Field>}
        {(sel || model) && <div className="start-what">
          <b>{sel?.title || model?.name}</b>
          <span className="muted small">{[sel?.file_name, hm(sel?.est_seconds || model?.est_seconds || 0), (sel?.material || '')].filter(x => x && x !== '—').join(' · ') || 'No G-code: the time is not known'}</span>
        </div>}
        <Field label="Filament in the printer" help={sel?.material && spools ? (() => { const s = spools.find(x => x.id === spoolId); return s && sel.material && s.material !== sel.material ? `⚠ This job wants ${sel.material}; ${s.material} is chosen.` : undefined })() : undefined}>
          <SpoolSelect value={spoolId} onChange={setSpoolId} spools={spools || []} placeholder="No spool (nothing is taken off)" />
        </Field>
        <Field label="Pieces on the plate"><NumberInput value={copies} onChange={v => setCopies(Math.max(1, Math.round(v)))} min={1} /></Field>
        {connected && <label className="check-row"><Switch checked={send} onChange={setSend} label="Send the G-code" /><span>Send the G-code to {p?.name} and start it there<br /><span className="muted small">Off: start it on the printer yourself; Print Farm follows it.</span></span></label>}
        {p?.needs_clear && <div className="warn-box"><Icon name="alert" size={15} />The last print may still be on {p.name}’s bed. Starting says it is clear.</div>}
      </div>
    </Modal>
  )
}

// ---------- finish a print ----------

export function FinishDialog({ print, initial = 'done', onClose, onDone }: { print: Print; initial?: 'done' | 'failed' | 'cancelled'; onClose: () => void; onDone?: (p: Print) => void }) {
  const { toast } = useToast()
  const [status, setStatus] = useState<'done' | 'failed' | 'cancelled'>(initial)
  const [reason, setReason] = useState(initial === 'failed' ? 'adhesion' : '')
  const [note, setNote] = useState('')
  const [gr, setGr] = useState(0)
  const [rating, setRating] = useState(0)
  const [busy, setBusy] = useState(false)
  const est = print.est_grams * (status === 'done' ? 1 : Math.max(print.progress, 0.05))
  const save = async () => {
    setBusy(true)
    try {
      const r = await api.post<Print>(`${V1}/prints/${print.id}/finish`, { status, fail_reason: status === 'done' ? '' : reason, fail_note: note, grams: gr, rating })
      toast(status === 'done' ? 'Done' : 'Written down'); announceChange(); onDone?.(r); onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={`How did “${print.title}” go?`} onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={busy}>Save</button>
    </>}>
      <div className="stack">
        <Segmented value={status} onChange={v => { setStatus(v); if (v === 'failed' && !reason) setReason('adhesion') }} options={[{ value: 'done', label: 'Done' }, { value: 'failed', label: 'Failed' }, { value: 'cancelled', label: 'I stopped it' }]} />
        {status !== 'done' && <Field label="What happened">
          <Select ariaLabel="Reason" value={reason} onChange={setReason} options={REASONS.map(r => ({ value: r.value, label: r.label }))} />
        </Field>}
        {status !== 'done' && <Field label="In your words"><input value={note} onChange={e => setNote(e.target.value)} placeholder="The corner lifted after 20 layers" /></Field>}
        {status === 'done' && <Field label="How good is it"><div className="stars-pick edit">{[1, 2, 3, 4, 5].map(n => <button key={n} type="button" className={n <= rating ? 'on' : ''} onClick={() => setRating(n === rating ? 0 : n)}>★</button>)}</div></Field>}
        <Field label="Filament used" help={est ? `Leave at 0 for about ${Math.round(est)} g (from the slicer${status !== 'done' ? ', for how far it got' : ''}).` : 'Taken off the spool that was in the printer.'}>
          <NumberInput value={gr} onChange={setGr} step={0.1} suffix="g" />
        </Field>
      </div>
    </Modal>
  )
}

// ---------- write down a print made earlier ----------

export function RecordDialog({ modelId, onClose }: { modelId?: number; onClose: () => void }) {
  const { toast } = useToast()
  const models = useList<Model>('/models?limit=1000&sort=name', 'models')
  const printers = useList<Printer>('/printers', 'printers')
  const spools = useList<Spool>('/spools', 'spools')
  const now = new Date()
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
  const [f, setF] = useState({ model_id: modelId || 0, title: '', printer_id: 0, spool_id: null as number | null, status: 'done' as 'done' | 'failed' | 'cancelled', started_at: local, hours: 0, grams: 0, fail_reason: 'adhesion', rating: 0, notes: '' })
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try {
      await api.post(`${V1}/prints`, { record: true, ...f, spool_id: f.spool_id || 0, fail_reason: f.status === 'done' ? '' : f.fail_reason, title: f.title || undefined })
      toast('Written down'); announceChange(); onClose()
    } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Write down a print" onClose={onClose} footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={busy || (!f.model_id && !f.title.trim())}>Save</button>
    </>}>
      <div className="stack">
        <p className="muted small">For a print made without Print Farm watching. Connected printers are written down by themselves.</p>
        <Field label="Model"><Select ariaLabel="Model" value={f.model_id ? String(f.model_id) : ''} onChange={v => setF({ ...f, model_id: Number(v) || 0 })} options={(models || []).map(m => ({ value: String(m.id), label: m.name }))} placeholder="From the library" clearLabel="Not in the library" /></Field>
        {!f.model_id && <Field label="What was printed"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></Field>}
        <div className="grid two">
          <Field label="Printer"><Select ariaLabel="Printer" value={f.printer_id ? String(f.printer_id) : ''} onChange={v => setF({ ...f, printer_id: Number(v) || 0 })} options={(printers || []).map(p => ({ value: String(p.id), label: p.name }))} placeholder="Choose" /></Field>
          <Field label="Spool"><SpoolSelect value={f.spool_id} onChange={v => setF({ ...f, spool_id: v })} spools={spools || []} /></Field>
        </div>
        <Segmented value={f.status} onChange={v => setF({ ...f, status: v })} options={[{ value: 'done', label: 'Done' }, { value: 'failed', label: 'Failed' }, { value: 'cancelled', label: 'Stopped' }]} />
        {f.status !== 'done' && <Field label="What happened"><Select ariaLabel="Reason" value={f.fail_reason} onChange={v => setF({ ...f, fail_reason: v })} options={REASONS.map(r => ({ value: r.value, label: r.label }))} /></Field>}
        <div className="grid two">
          <Field label="Started"><input type="datetime-local" value={f.started_at} onChange={e => setF({ ...f, started_at: e.target.value })} /></Field>
          <Field label="It took" help="0 = the slicer's time"><NumberInput value={f.hours} onChange={v => setF({ ...f, hours: v })} step={0.25} suffix="h" /></Field>
        </div>
        <Field label="Filament used" help="0 = the slicer's grams"><NumberInput value={f.grams} onChange={v => setF({ ...f, grams: v })} step={0.1} suffix="g" /></Field>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

// ---------- spools and filaments ----------

const emptyFil = { brand: '', material: 'PLA', name: '', color: '#888888', diameter: 1.75, nozzle_temp: 0, bed_temp: 0, net_weight: 1000, spool_weight: 0, price: 0, url: '', notes: '' }

function FilamentFields({ f, setF, currency }: { f: typeof emptyFil; setF: (v: typeof emptyFil) => void; currency?: string }) {
  return (
    <>
      <div className="grid two">
        <Field label="Material"><Select ariaLabel="Material" value={f.material} onChange={v => setF({ ...f, material: v })} options={MATERIALS.map(m => ({ value: m, label: m }))} onCreate={v => v.toUpperCase()} createLabel="Use" /></Field>
        <Field label="Brand"><input value={f.brand} onChange={e => setF({ ...f, brand: e.target.value })} placeholder="Prusament, Polymaker…" /></Field>
      </div>
      <div className="grid two">
        <Field label="Colour"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="Galaxy Black" /></Field>
        <Field label="Shade"><div className="row"><input type="color" className="color-in" value={f.color || '#888888'} onChange={e => setF({ ...f, color: e.target.value })} /><span className="muted small tnum">{f.color}</span></div></Field>
      </div>
      <div className="grid three">
        <Field label="Nozzle"><NumberInput value={f.nozzle_temp} onChange={v => setF({ ...f, nozzle_temp: Math.round(v) })} suffix="°C" /></Field>
        <Field label="Bed"><NumberInput value={f.bed_temp} onChange={v => setF({ ...f, bed_temp: Math.round(v) })} suffix="°C" /></Field>
        <Field label="Diameter"><NumberInput value={f.diameter} onChange={v => setF({ ...f, diameter: v })} step={0.05} suffix="mm" /></Field>
      </div>
      <div className="grid three">
        <Field label="On a new spool"><NumberInput value={f.net_weight} onChange={v => setF({ ...f, net_weight: Math.round(v) })} suffix="g" /></Field>
        <Field label="Empty spool" help="For weighing"><NumberInput value={f.spool_weight} onChange={v => setF({ ...f, spool_weight: Math.round(v) })} suffix="g" /></Field>
        <Field label="Price"><MoneyInput value={f.price} onChange={v => setF({ ...f, price: v })} currency={currency} /></Field>
      </div>
    </>
  )
}

export function FilamentDialog({ filament, onClose }: { filament?: Filament; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState(filament ? { ...emptyFil, ...filament, color: filament.color || '#888888' } : { ...emptyFil, material: settings?.default_material || 'PLA' })
  const save = async () => {
    try { filament ? await api.put(`${V1}/filaments/${filament.id}`, f) : await api.post(`${V1}/filaments`, f); toast('Saved'); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => {
    if (!filament || !confirm(`Remove ${filament.material} ${filament.name} and its ${filament.spools} spools?`)) return
    try { await api.del(`${V1}/filaments/${filament.id}`); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title={filament ? 'Edit the filament' : 'A new filament'} onClose={onClose} footer={<>
      {filament && <button type="button" className="btn danger ghost" onClick={remove}><Icon name="trash" size={15} />Remove</button>}
      <span className="spacer" />
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save}>Save</button>
    </>}>
      <div className="stack"><FilamentFields f={f} setF={setF} currency={settings?.currency} /><Field label="Where to buy it"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://" /></Field></div>
    </Modal>
  )
}

export function SpoolDialog({ spool, onClose }: { spool?: Spool; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const filaments = useList<Filament>('/filaments', 'filaments')
  const [newFil, setNewFil] = useState(false)
  const [nf, setNf] = useState({ ...emptyFil, material: settings?.default_material || 'PLA' })
  const [f, setF] = useState({ filament_id: spool?.filament_id || 0, initial_g: spool?.initial_g || 1000, left_g: spool ? Math.round(spool.left_g) : 1000, price: spool?.price || 0, bought_on: spool?.bought_on || todayISO(),
    opened_on: spool?.opened_on || '', dried_on: spool?.dried_on || '', location: spool?.location || '', lot: spool?.lot || '', notes: spool?.notes || '', archived: spool?.archived || false, count: 1 })
  useEffect(() => { if (filaments && !filaments.length && !spool) setNewFil(true) }, [filaments, spool])
  const pick = (id: number) => {
    const x = filaments?.find(y => y.id === id)
    setF({ ...f, filament_id: id, initial_g: x?.net_weight || f.initial_g, left_g: spool ? f.left_g : (x?.net_weight || f.left_g), price: f.price || x?.price || 0 })
  }
  const save = async () => {
    try {
      const body: Record<string, unknown> = { ...f }
      if (newFil && !spool) { body.filament = nf; delete body.filament_id; body.initial_g = nf.net_weight; body.left_g = nf.net_weight; body.price = f.price || nf.price }
      if (spool) await api.put(`${V1}/spools/${spool.id}`, body); else await api.post(`${V1}/spools`, body)
      toast(spool ? 'Saved' : f.count > 1 ? `${f.count} spools on the shelf` : 'On the shelf'); announceChange(); onClose()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async () => {
    if (!spool || !confirm('Remove this spool? (To keep its history, mark it empty instead.)')) return
    try { await api.del(`${V1}/spools/${spool.id}`); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') }
  }
  return (
    <Modal title={spool ? 'Edit the spool' : 'Add spools'} onClose={onClose} footer={<>
      {spool && <button type="button" className="btn danger ghost" onClick={remove}><Icon name="trash" size={15} />Remove</button>}
      <span className="spacer" />
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={!newFil && !f.filament_id}>Save</button>
    </>}>
      <div className="stack">
        {!spool && <Segmented value={newFil ? 'new' : 'have'} onChange={v => setNewFil(v === 'new')} options={[{ value: 'have', label: 'A filament I have', disabled: !filaments?.length }, { value: 'new', label: 'A new filament' }]} />}
        {newFil && !spool ? <FilamentFields f={nf} setF={setNf} currency={settings?.currency} /> : (
          <Field label="Filament">
            <Select ariaLabel="Filament" value={f.filament_id ? String(f.filament_id) : ''} onChange={v => pick(Number(v))} options={(filaments || []).map(x => ({ value: String(x.id), label: `${x.material} ${x.name}`.trim(), hint: x.brand, group: x.material }))} placeholder="Choose" />
          </Field>
        )}
        {spool && <div className="grid two">
          <Field label="On it when new"><NumberInput value={f.initial_g} onChange={v => setF({ ...f, initial_g: v })} suffix="g" /></Field>
          <Field label="Left now"><NumberInput value={f.left_g} onChange={v => setF({ ...f, left_g: v })} suffix="g" /></Field>
        </div>}
        <div className="grid two">
          {!spool && <Field label="How many"><NumberInput value={f.count} onChange={v => setF({ ...f, count: Math.max(1, Math.round(v)) })} min={1} /></Field>}
          <Field label="Price each" help={!spool && settings?.money_on_spool ? 'Written down in Pocket Money too.' : undefined}><MoneyInput value={f.price} onChange={v => setF({ ...f, price: v })} currency={settings?.currency} /></Field>
        </div>
        <div className="grid two">
          <Field label="Bought"><input type="date" value={f.bought_on} onChange={e => setF({ ...f, bought_on: e.target.value })} /></Field>
          <Field label="Where it is"><input value={f.location} onChange={e => setF({ ...f, location: e.target.value })} placeholder="Shelf A, dry box" /></Field>
        </div>
        {spool && <div className="grid two">
          <Field label="Opened"><input type="date" value={f.opened_on} onChange={e => setF({ ...f, opened_on: e.target.value })} /></Field>
          <Field label="Last dried"><input type="date" value={f.dried_on} onChange={e => setF({ ...f, dried_on: e.target.value })} /></Field>
        </div>}
        <Field label="Notes"><input value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
        {spool && <label className="check-row"><Switch checked={f.archived} onChange={v => setF({ ...f, archived: v })} label="Empty" /><span>Empty or gone (kept for the history)</span></label>}
      </div>
    </Modal>
  )
}

export function WeighDialog({ spool, onClose }: { spool: Spool; onClose: () => void }) {
  const { toast } = useToast()
  const [mode, setMode] = useState<'gross' | 'left' | 'used'>(spool.spool_weight > 0 ? 'gross' : 'left')
  const [v, setV] = useState(0)
  const save = async () => {
    try {
      await api.post(`${V1}/spools/${spool.id}/weigh`, mode === 'gross' ? { gross_g: v } : mode === 'left' ? { left_g: v } : { used_g: v, note: 'Used by hand' })
      toast('Saved'); announceChange(); onClose()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const after = mode === 'gross' ? v - spool.spool_weight : mode === 'left' ? v : spool.left_g - v
  return (
    <Modal title={`Weigh ${spool.material} ${spool.name}`} onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={!v}>Save</button>
    </>}>
      <div className="stack">
        <Segmented value={mode} onChange={setMode} options={[{ value: 'gross', label: 'On the scale', disabled: !spool.spool_weight }, { value: 'left', label: 'Left on it' }, { value: 'used', label: 'Used' }]} />
        <Field label={mode === 'gross' ? 'The spool weighs' : mode === 'left' ? 'Filament left' : 'Filament used (a print not seen)'} help={mode === 'gross' ? `With the empty spool (${spool.spool_weight} g).` : undefined}>
          <NumberInput value={v} onChange={setV} step={1} suffix="g" />
        </Field>
        {!!v && <p className="muted small">Then {Math.max(0, Math.round(after))} g is left (now {Math.round(spool.left_g)} g).</p>}
      </div>
    </Modal>
  )
}

// ---------- a printer ----------

export function PrinterDialog({ printer, onClose, onSaved }: { printer?: Printer; onClose: () => void; onSaved?: (p: Printer) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const state = useAppState()
  const homeOn = state('home', 'home:read').ok
  const [f, setF] = useState({
    name: printer?.name || '', model: printer?.model || '', kind: printer?.kind || 'fdm', connection: printer?.connection || 'manual', url: printer?.url || '', api_key: printer?.has_key ? '••••' : '',
    webcam_url: printer?.webcam_url || '', bed_x: printer?.bed_x || 0, bed_y: printer?.bed_y || 0, bed_z: printer?.bed_z || 0, nozzle: printer?.nozzle || 0.4, materials: printer?.materials || 'PLA, PETG',
    watts: printer?.watts || 0, wear_per_hour: printer?.wear_per_hour || 0, price: printer?.price || 0, bought_on: printer?.bought_on || '', start_hours: printer?.start_hours || 0,
    location: printer?.location || '', color: printer?.color || '#e8641b', notes: printer?.notes || '', home_ref: printer?.home_ref || '', archived: printer?.archived || false,
  })
  const [test, setTest] = useState('')
  const [things, setThings] = useState<{ id: string; name: string; room: string }[] | null>(null)
  useEffect(() => { if (homeOn) api.get<{ things: { id: string; name: string; room: string }[] }>(`${V1}/apps/home/things`).then(r => setThings(r.things)).catch(() => setThings([])) }, [homeOn])
  const runTest = async () => {
    setTest('…')
    try {
      const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/printers/test`, { id: printer?.id || 0, connection: f.connection, url: f.url, api_key: f.api_key })
      setTest(r.ok ? '✓ ' + r.message : '✕ ' + r.error)
    } catch (e) { setTest('✕ ' + errMsg(e)) }
  }
  const save = async () => {
    try {
      const p = printer ? await api.put<{ printer: Printer }>(`${V1}/printers/${printer.id}`, f) : await api.post<{ printer: Printer }>(`${V1}/printers`, f)
      toast('Saved'); announceChange(); onSaved?.(p.printer); onClose()
    } catch (e) { toast(errMsg(e), 'err') }
  }
  const conn = CONNECTIONS.find(c => c.value === f.connection)
  const keyHelp: Record<string, string> = { moonraker: 'Only if Moonraker asks for one (most do not on a home network).', octoprint: 'OctoPrint → Settings → Application keys (or the global API key).', prusalink: 'The printer’s screen: Settings → Network → PrusaLink, the API key.' }
  const urlHelp: Record<string, string> = { moonraker: 'Like http://voron.local or http://192.168.1.40:7125', octoprint: 'Like http://octopi.local', prusalink: 'The printer’s address, like http://192.168.1.52' }
  return (
    <Modal title={printer ? `Edit ${printer.name}` : 'Add a printer'} onClose={onClose} size="lg" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={save} disabled={!f.name.trim()}>Save</button>
    </>}>
      <div className="stack">
        <div className="grid two">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} placeholder="Mk4" autoFocus /></Field>
          <Field label="Model"><input value={f.model} onChange={e => setF({ ...f, model: e.target.value })} placeholder="Prusa MK4S, Voron 2.4, Bambu Lab P1S…" /></Field>
        </div>
        <Field label="How Print Farm reaches it" help={conn?.hint}>
          <Select ariaLabel="Connection" value={f.connection} onChange={v => setF({ ...f, connection: v as typeof f.connection })} options={CONNECTIONS.map(c => ({ value: c.value, label: c.label, hint: c.hint }))} />
        </Field>
        {f.connection !== 'manual' && <>
          <div className="grid two">
            <Field label="Address" help={urlHelp[f.connection]}><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="http://192.168.1.50" /></Field>
            <Field label="API key" help={keyHelp[f.connection]}><input value={f.api_key} onChange={e => setF({ ...f, api_key: e.target.value })} placeholder={f.connection === 'moonraker' ? 'usually none' : ''} autoComplete="off" /></Field>
          </div>
          <div className="row wrap"><button type="button" className="btn sm" onClick={runTest} disabled={!f.url}><Icon name="refresh" size={14} />Ask the printer</button><span className={`small ${test.startsWith('✓') ? 'ok' : test.startsWith('✕') ? 'bad' : 'muted'}`}>{test}</span></div>
        </>}
        <Field label="Camera" help={f.connection === 'moonraker' ? 'Empty = the camera Moonraker knows. Or a snapshot address (JPEG).' : 'A snapshot address that gives one picture (JPEG), like http://octopi.local/webcam/?action=snapshot'}>
          <input value={f.webcam_url} onChange={e => setF({ ...f, webcam_url: e.target.value })} placeholder="http://…/snapshot" />
        </Field>
        <div className="grid four">
          <Field label="Bed X"><NumberInput value={f.bed_x} onChange={v => setF({ ...f, bed_x: v })} suffix="mm" /></Field>
          <Field label="Bed Y"><NumberInput value={f.bed_y} onChange={v => setF({ ...f, bed_y: v })} suffix="mm" /></Field>
          <Field label="Height"><NumberInput value={f.bed_z} onChange={v => setF({ ...f, bed_z: v })} suffix="mm" /></Field>
          <Field label="Nozzle"><NumberInput value={f.nozzle} onChange={v => setF({ ...f, nozzle: v })} step={0.1} suffix="mm" /></Field>
        </div>
        <Field label="It prints" help="Jobs that want another material go to another printer."><input value={f.materials} onChange={e => setF({ ...f, materials: e.target.value })} placeholder="PLA, PETG, TPU" /></Field>
        <h4 className="sub-h">What it costs to run</h4>
        <div className="grid three">
          <Field label="Power while printing" help={`At ${settings?.kwh_price ?? 0.25} ${settings?.currency || ''}/kWh (Settings)`}><NumberInput value={f.watts} onChange={v => setF({ ...f, watts: Math.round(v) })} suffix="W" /></Field>
          <Field label="Wear per hour" help="Nozzles, belts, the printer itself"><MoneyInput value={f.wear_per_hour} onChange={v => setF({ ...f, wear_per_hour: v })} currency={settings?.currency} /></Field>
          <Field label="Hours before Print Farm"><NumberInput value={f.start_hours} onChange={v => setF({ ...f, start_hours: v })} suffix="h" /></Field>
        </div>
        <div className="grid three">
          <Field label="Bought"><input type="date" value={f.bought_on} onChange={e => setF({ ...f, bought_on: e.target.value })} /></Field>
          <Field label="Price"><MoneyInput value={f.price} onChange={v => setF({ ...f, price: v })} currency={settings?.currency} /></Field>
          <Field label="Where it stands"><input value={f.location} onChange={e => setF({ ...f, location: e.target.value })} placeholder="Workshop" /></Field>
        </div>
        <Field label="Its appliance in Pocket Home" help={homeOn ? 'Upkeep done can go into its log there.' : undefined}>
          {homeOn ? <Select ariaLabel="Appliance" value={f.home_ref} onChange={v => setF({ ...f, home_ref: v })} options={(things || []).map(t => ({ value: t.id, label: t.name, hint: t.room }))} placeholder="Not linked" clearLabel="Not linked" />
            : <div className="row"><Select ariaLabel="Appliance" value="" onChange={() => {}} options={[]} placeholder="Needs Pocket Home" disabled /><CoreHint /></div>}
        </Field>
        <div className="grid two">
          <Field label="Colour on the farm"><div className="row"><input type="color" className="color-in" value={f.color} onChange={e => setF({ ...f, color: e.target.value })} /></div></Field>
          {printer && <Field label="Put away"><label className="check-row"><Switch checked={f.archived} onChange={v => setF({ ...f, archived: v })} label="Archived" /><span className="small">Not on the farm any more (its history stays)</span></label></Field>}
        </div>
        <Field label="Notes"><textarea rows={2} value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
      </div>
    </Modal>
  )
}

// ---------- models in ----------

type Added = { added: { model: Model; file: ModelFile; new: boolean }[]; errors?: string[] }

export function AddModelsDialog({ modelId, onClose, onAdded }: { modelId?: number; onClose: () => void; onAdded?: (models: Model[]) => void }) {
  const { toast } = useToast()
  const state = useAppState()
  const filesOn = state('files', 'files:read').ok
  const [tab, setTab] = useState<'upload' | 'link' | 'files'>('upload')
  const [files, setFiles] = useState<File[]>([])
  const [together, setTogether] = useState(!!modelId)
  const [link, setLink] = useState('')
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const done = (r: Added) => {
    const n = r.added.length
    if (r.errors?.length) toast(r.errors.join('; '), 'err')
    toast(`${n} ${n === 1 ? 'file' : 'files'} in the library`)
    announceChange(); onAdded?.(r.added.map(a => a.model)); onClose()
  }
  const send = async () => {
    setBusy(true)
    try { done(await uploadFiles<Added>(`${V1}/models/upload`, files, { ...(modelId ? { model_id: String(modelId) } : {}), together: together ? '1' : '0' })) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const fromLink = async () => {
    setBusy(true)
    try { const r = await api.post<{ model: Model; file: ModelFile; new: boolean }>(`${V1}/models/link`, { url: link, model_id: modelId || 0 }); done({ added: [r] }) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const pick = (l: FileList | null) => { if (l) setFiles(fs => [...fs, ...Array.from(l)]) }
  return (
    <Modal title={modelId ? 'Add files to the model' : 'Add models'} onClose={onClose} size="lg" footer={tab === 'upload' ? <>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={send} disabled={busy || !files.length}>{busy ? <Spinner /> : <Icon name="upload" size={15} />}Add {files.length || ''}</button>
    </> : tab === 'link' ? <>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={fromLink} disabled={busy || !link.trim()}>{busy ? <Spinner /> : <Icon name="download" size={15} />}Bring it in</button>
    </> : undefined}>
      <div className="stack">
        <Tabs tabs={[{ id: 'upload', label: 'From this device', icon: 'upload' }, { id: 'link', label: 'A link', icon: 'link' }, { id: 'files', label: 'Pocket Files', icon: 'archive' }]} value={tab} onChange={setTab} />
        {tab === 'upload' && <>
          <div className={`drop ${over ? 'over' : ''}`} onClick={() => input.current?.click()} onDragOver={e => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)} onDrop={e => { e.preventDefault(); setOver(false); pick(e.dataTransfer.files) }}>
            <Icon name="upload" size={26} />
            <b>Drop STL, 3MF, OBJ, STEP or G-code files here</b>
            <span className="muted small">or press to choose them. G-code brings the slicer’s time, grams, settings and picture.</span>
            <input ref={input} type="file" multiple hidden accept=".stl,.3mf,.obj,.step,.stp,.gcode,.gco,.bgcode" onChange={e => pick(e.target.files)} />
          </div>
          {!!files.length && <ul className="file-list">{files.map((f, i) => <li key={i}><Icon name="file" size={14} /><span className="ellipsis">{f.name}</span><span className="muted">{fmtBytes(f.size)}</span><button type="button" className="btn ghost icon-only sm" aria-label="Remove" onClick={() => setFiles(fs => fs.filter((_, j) => j !== i))}><Icon name="x" size={13} /></button></li>)}</ul>}
          {!modelId && files.length > 1 && <label className="check-row"><Switch checked={together} onChange={setTogether} label="One model" /><span>All in one model (the parts of one thing, or its STL and G-code)</span></label>}
        </>}
        {tab === 'link' && <Field label="A link to the file" help="The download link of the .stl, .3mf or .gcode (on Printables or Thingiverse: the address of the download button, not the page).">
          <input value={link} onChange={e => setLink(e.target.value)} placeholder="https://…/phone_stand.stl" autoFocus />
        </Field>}
        {tab === 'files' && (filesOn ? <FilesBrowser modelId={modelId} onDone={done} /> : <div className="muted">Pocket Files is not connected. <CoreHint /></div>)}
      </div>
    </Modal>
  )
}

function FilesBrowser({ modelId, onDone }: { modelId?: number; onDone: (r: Added) => void }) {
  const { toast } = useToast()
  const [space, setSpace] = useState('home')
  const [path, setPath] = useState('/')
  const [list, setList] = useState<{ entries: { name: string; path: string; kind: string; size: number }[]; spaces: { id: string; name: string }[] } | null>(null)
  const [sel, setSel] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    setList(null)
    api.get<typeof list>(`${V1}/apps/files?space=${encodeURIComponent(space)}&path=${encodeURIComponent(path)}`).then(setList).catch(e => toast(errMsg(e), 'err'))
  }, [space, path]) // eslint-disable-line react-hooks/exhaustive-deps
  const up = path === '/' ? '' : path.replace(/\/[^/]+\/?$/, '') || '/'
  const go = async () => {
    setBusy(true)
    try { onDone(await api.post<Added>(`${V1}/apps/files/import`, { space, paths: sel, model_id: modelId || 0, together: !!modelId })) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const crumbs = useMemo(() => path.split('/').filter(Boolean), [path])
  return (
    <div className="stack">
      <div className="row wrap">
        {list && list.spaces.length > 1 && <Select ariaLabel="Space" value={space} onChange={v => { setSpace(v); setPath('/') }} options={list.spaces.map(s => ({ value: s.id, label: s.name }))} />}
        <span className="muted small">/{crumbs.join('/')}</span>
      </div>
      <div className="pick-list short">
        {up && <button type="button" className="pick-row" onClick={() => setPath(up)}><Icon name="arrow-left" size={15} />Back</button>}
        {!list ? <Spinner /> : list.entries.length === 0 ? <div className="muted small pad">No model files here.</div> : list.entries.map(e => e.kind === 'folder'
          ? <button key={e.path} type="button" className="pick-row" onClick={() => setPath(e.path)}><Icon name="archive" size={15} /><span className="grow ellipsis">{e.name}</span><Icon name="chevron-right" size={14} /></button>
          : <label key={e.path} className="pick-row"><input type="checkbox" checked={sel.includes(e.path)} onChange={x => setSel(s => x.target.checked ? [...s, e.path] : s.filter(p => p !== e.path))} /><Icon name="file" size={15} /><span className="grow ellipsis">{e.name}</span><span className="muted small">{kindLabel(e.kind)} · {fmtBytes(e.size)}</span></label>)}
      </div>
      <div className="row"><span className="spacer" /><button type="button" className="btn primary" disabled={!sel.length || busy} onClick={go}>{busy ? <Spinner /> : <Icon name="download" size={15} />}Bring {sel.length || ''} in</button></div>
    </div>
  )
}

/** The farm's currency, for amounts in lists. */
export function useMoney() {
  const { settings } = useApp()
  const cur = settings?.currency || 'EUR'
  return (minor: number) => money(minor, cur)
}

