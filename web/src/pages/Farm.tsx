// The farm at a glance: every printer as it is now, what needs you, and
// what is next in the queue.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { endsAt, hm, hmShort } from '../lib/farm'
import type { Farm, Job, PrinterView, Spool, Upkeep } from '../lib/types'
import { Confirm, Empty, Loading, useToast, errMsg, MenuButton, type MenuItem } from '../components/ui'
import { Icon } from '../components/icons'
import { Bar, Cover, SpoolChip, StateBadge, Swatch, Temps } from '../components/bits'
import { FinishDialog, JobDialog, PrinterDialog, StartDialog } from '../components/dialogs'

export function usePoll(fn: () => void, ms: number) {
  const ref = useRef(fn)
  ref.current = fn
  useEffect(() => {
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') ref.current() }, ms)
    return () => window.clearInterval(t)
  }, [ms])
}

export default function FarmPage() {
  const { settings } = useApp()
  const navigate = useNavigate()
  const [farm, setFarm] = useState<Farm | null>(null)
  const [low, setLow] = useState<Spool[]>([])
  const [due, setDue] = useState<Upkeep[]>([])
  const [adding, setAdding] = useState(false)
  const [starting, setStarting] = useState<{ printer?: PrinterView; job?: Job } | null>(null)
  const [queueing, setQueueing] = useState(false)
  const load = useCallback(() => {
    api.get<Farm>(`${V1}/farm`).then(setFarm).catch(() => {})
  }, [])
  const loadSide = useCallback(() => {
    api.get<{ spools: Spool[]; low_grams: number }>(`${V1}/spools`).then(r => setLow(r.spools.filter(s => s.left_g < r.low_grams))).catch(() => {})
    api.get<{ upkeep: Upkeep[] }>(`${V1}/upkeep`).then(r => setDue(r.upkeep.filter(u => u.due))).catch(() => {})
  }, [])
  useEffect(() => { load(); loadSide() }, [load, loadSide])
  useOnChange(useCallback(() => { load(); loadSide() }, [load, loadSide]))
  usePoll(load, 5000)

  if (!farm) return <Loading />
  const printing = farm.printers.filter(p => p.print).length
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow">
          <h2 className="page-title">The farm</h2>
          <p className="muted">{farm.printers.length ? `${printing} of ${farm.printers.length} printing · ${farm.queued} in the queue` : 'Add your printers to get started.'}</p>
        </div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={() => setQueueing(true)}><Icon name="list-plus" size={15} /><span className="hide-sm">Add to the queue</span></button>
          <button type="button" className="btn primary" onClick={() => setStarting({})} disabled={!farm.printers.length}><Icon name="play" size={15} />Start a print</button>
        </div>
      </div>

      {(low.length > 0 || due.length > 0) && <div className="attention">
        {low.length > 0 && <Link to="/filament" className="att-card warn">
          <Icon name="disc" size={18} />
          <div className="grow min0"><b>{low.length} {low.length === 1 ? 'spool is' : 'spools are'} running low</b><span className="muted small ellipsis block-line">{low.slice(0, 3).map(s => `${s.material} ${s.name} (${Math.round(s.left_g)} g)`).join(', ')}</span></div>
          <Icon name="chevron-right" size={16} />
        </Link>}
        {due.length > 0 && <Link to="/upkeep" className="att-card">
          <Icon name="wrench" size={18} />
          <div className="grow min0"><b>Upkeep is due</b><span className="muted small ellipsis block-line">{due.slice(0, 3).map(u => `${u.printer}: ${u.title}`).join(', ')}</span></div>
          <Icon name="chevron-right" size={16} />
        </Link>}
      </div>}

      {farm.printers.length === 0 ? (
        <Empty title="No printers yet" icon="printer">
          Add a printer: connected through Klipper (Moonraker), OctoPrint or PrusaLink, Print Farm follows its prints by itself; by hand, you say when a print starts and ends.
          <div className="mt"><button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="plus" size={15} />Add a printer</button></div>
        </Empty>
      ) : (
        <div className="printer-grid">
          {farm.printers.map(p => <PrinterCard key={p.id} p={p} onStart={job => setStarting({ printer: p, job })} onChanged={load} />)}
          <button type="button" className="printer-add" onClick={() => setAdding(true)}><Icon name="plus" size={20} />Add a printer</button>
        </div>
      )}

      <section className="card">
        <div className="card-head"><h3>Up next</h3><span className="spacer" /><Link to="/queue" className="btn ghost sm">The queue<Icon name="chevron-right" size={14} /></Link></div>
        {farm.queue.length === 0 ? <div className="pad muted">The queue is empty. <button type="button" className="linkish" onClick={() => setQueueing(true)}>Add a job</button></div> : (
          <ul className="list">
            {farm.queue.slice(0, 6).map(j => (
              <li key={j.id}>
                <div className="list-row">
                  <Cover src={j.has_cover && j.model_id ? `${V1}/models/${j.model_id}/cover` : false} size={40} />
                  <div className="grow min0">
                    <div className="ellipsis"><b>{j.title}</b>{j.quantity > 1 && <span className="muted"> · {j.done_count} of {j.quantity}</span>}</div>
                    <div className="muted small ellipsis">{[j.printer || 'any printer', j.filament || j.material, j.est_seconds ? hm(j.est_seconds) : '', j.for_whom && `for ${j.for_whom}`].filter(Boolean).join(' · ')}</div>
                  </div>
                  {j.color && <Swatch color={j.color} />}
                  {j.status === 'printing' ? <span className="badge accent">printing</span> : <button type="button" className="btn sm" onClick={() => setStarting({ job: j })}><Icon name="play" size={13} />Start</button>}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      {adding && <PrinterDialog onClose={() => setAdding(false)} onSaved={p => navigate(`/printers/${p.id}`)} />}
      {starting && <StartDialog printer={starting.printer} job={starting.job} onClose={() => setStarting(null)} onStarted={() => { load(); announceChange() }} />}
      {queueing && <JobDialog onClose={() => setQueueing(false)} />}
      {settings?.demo && <p className="muted small center">Demo data: the printers are “by hand” here, so the time left comes from the slicer’s estimate.</p>}
    </div>
  )
}

export function PrinterCard({ p, onStart, onChanged, big = false }: { p: PrinterView; onStart: (job?: Job) => void; onChanged: () => void; big?: boolean }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [finish, setFinish] = useState<'done' | 'failed' | null>(null)
  const [stop, setStop] = useState(false)
  const [busy, setBusy] = useState(false)
  const control = async (action: string) => {
    setBusy(true)
    try { await api.post(`${V1}/printers/${p.id}/control`, { action }); onChanged(); announceChange() } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  const pr = p.print
  const connected = p.connection !== 'manual'
  const menu = (): MenuItem[] => [
    { label: 'Open', icon: 'printer', onSelect: () => navigate(`/printers/${p.id}`) },
    ...(pr ? [{ label: 'Open the print', icon: 'file', onSelect: () => navigate(`/prints/${pr.id}`) }] : []),
    ...(connected ? [{ label: 'Ask it now', icon: 'refresh', onSelect: () => control('refresh') }] : []),
    ...(pr && !connected ? [{ label: 'It failed…', icon: 'x-circle', onSelect: () => setFinish('failed') }] : []),
  ]
  return (
    <article className={`printer-card ${big ? 'big' : ''} state-${p.state}`} style={{ '--pc': p.color || 'var(--accent)' } as React.CSSProperties}>
      <header className="pc-head">
        <Link to={`/printers/${p.id}`} className="pc-name min0"><b className="ellipsis">{p.name}</b><span className="muted small ellipsis">{p.model || (connected ? p.live?.software : 'By hand')}</span></Link>
        <StateBadge state={p.overdue ? 'done' : p.state} label={p.overdue ? 'Should be done' : undefined} />
        <MenuButton label="More" items={menu} align="right"><Icon name="more" size={16} /></MenuButton>
      </header>
      {p.camera && <Camera id={p.id} live={p.state === 'printing'} />}
      <div className="pc-body">
        {pr ? <>
          <Link to={`/prints/${pr.id}`} className="pc-print ellipsis">{pr.title}</Link>
          <Bar value={p.progress} tone={p.state === 'paused' ? 'warn' : ''} />
          <div className="pc-nums tnum">
            <b>{Math.round(p.progress * 100)}%</b>
            <span>{p.left > 0 ? `${hmShort(p.left)} left` : p.overdue ? 'past its time' : ''}</span>
            {p.ends_at && <span className="muted">done {endsAt(p.ends_at)}</span>}
          </div>
          {p.live && p.live.layers > 0 && <div className="muted small tnum">Layer {p.live.layer} of {p.live.layers}</div>}
        </> : p.needs_clear ? <div className="pc-idle"><Icon name="check-circle" size={16} className="ok" />A print is waiting on the bed.</div>
          : p.state === 'offline' ? <div className="pc-idle muted"><Icon name="x-circle" size={16} />{p.live_error || 'Not answering.'}</div>
            : p.state === 'error' ? <div className="pc-idle bad"><Icon name="alert" size={16} />{p.live?.message || 'The printer shows an error.'}</div>
              : p.next ? <div className="pc-idle"><span className="muted">Next:</span> <b className="ellipsis">{p.next.title}</b></div>
                : <div className="pc-idle muted">Nothing to print for it in the queue.</div>}
        <div className="pc-meta">
          <SpoolChip spool={p.spool} low={settings?.low_grams} />
          {p.live && p.state !== 'offline' && <Temps nozzle={p.live.nozzle} nozzleTo={p.live.nozzle_target} bed={p.live.bed} bedTo={p.live.bed_target} />}
          {p.upkeep_due > 0 && <Link to={`/printers/${p.id}`} className="badge warn" title="Upkeep is due"><Icon name="wrench" size={11} />{p.upkeep_due}</Link>}
        </div>
      </div>
      <footer className="pc-foot">
        {pr ? <>
          {p.state === 'paused' ? <button type="button" className="btn sm" disabled={busy} onClick={() => control('resume')}><Icon name="play" size={13} />Go on</button>
            : <button type="button" className="btn sm" disabled={busy} onClick={() => control('pause')}><Icon name="pause" size={13} />Pause</button>}
          <button type="button" className="btn sm" disabled={busy} onClick={() => setStop(true)}><Icon name="stop" size={13} />Stop</button>
          <span className="spacer" />
          {!connected && <button type="button" className="btn sm primary" onClick={() => setFinish('done')}><Icon name="check" size={13} />Done</button>}
        </> : p.needs_clear ? <>
          <button type="button" className="btn sm primary" disabled={busy} onClick={() => control('clear')}><Icon name="check" size={13} />The bed is clear</button>
        </> : <>
          <button type="button" className="btn sm primary" disabled={p.state === 'offline' || p.state === 'error'} onClick={() => onStart(p.next || undefined)}><Icon name="play" size={13} />{p.next ? 'Start the next' : 'Start a print'}</button>
        </>}
      </footer>
      {finish && pr && <FinishDialog print={pr} initial={finish} onClose={() => setFinish(null)} onDone={onChanged} />}
      {stop && pr && <Confirm title={`Stop “${pr.title}”?`} message={connected ? `${p.name} stops printing now. This cannot be undone.` : 'It is written down as stopped.'} confirmLabel="Stop it" onCancel={() => setStop(false)} onConfirm={() => { setStop(false); control('cancel') }} />}
    </article>
  )
}

/** The camera: a new picture every few seconds while printing, without moving anything. */
export function Camera({ id, live }: { id: number; live: boolean }) {
  const [t, setT] = useState(Date.now())
  const [shown, setShown] = useState('')
  const [bad, setBad] = useState(false)
  usePoll(() => { if (live) setT(Date.now()) }, 8000)
  const src = `${V1}/printers/${id}/snapshot?t=${t}`
  useEffect(() => {
    const img = new Image()
    img.onload = () => { setShown(src); setBad(false) }
    img.onerror = () => setBad(true)
    img.src = src
  }, [src])
  return (
    <div className="pc-cam">
      {shown ? <img src={shown} alt="The printer's camera" /> : <div className="pc-cam-msg muted small">{bad ? <><Icon name="camera" size={16} />The camera does not answer</> : <Icon name="camera" size={16} />}</div>}
    </div>
  )
}
