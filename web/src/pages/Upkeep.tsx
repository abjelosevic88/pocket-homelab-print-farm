// Upkeep for every printer: what is due, what is coming, the log book.
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import { money } from '../lib/farm'
import type { Printer, Upkeep, UpkeepLog } from '../lib/types'
import { fmtDate } from '../lib/format'
import { Empty, Loading } from '../components/ui'
import { Icon } from '../components/icons'
import { UpkeepDialog, UpkeepDoneDialog, UpkeepRow } from './Printers'

export default function UpkeepPage() {
  const { settings } = useApp()
  const [d, setD] = useState<{ upkeep: Upkeep[]; log: UpkeepLog[] } | null>(null)
  const [printers, setPrinters] = useState<Printer[]>([])
  const [edit, setEdit] = useState<Upkeep | 'new' | null>(null)
  const [done, setDone] = useState<Upkeep | null>(null)
  const load = useCallback(() => {
    api.get<{ upkeep: Upkeep[]; log: UpkeepLog[] }>(`${V1}/upkeep`).then(setD).catch(() => {})
    api.get<{ printers: Printer[] }>(`${V1}/printers`).then(r => setPrinters(r.printers)).catch(() => {})
  }, [])
  useEffect(load, [load])
  useOnChange(load)
  if (!d) return <Loading />
  const due = d.upkeep.filter(u => u.due), soon = d.upkeep.filter(u => u.soon), rest = d.upkeep.filter(u => !u.due && !u.soon)
  const cur = settings?.currency || 'EUR'
  const spent = d.log.reduce((a, l) => a + l.cost, 0)
  const group = (title: string, list: Upkeep[]) => list.length > 0 && <section className="card">
    <div className="card-head"><h3>{title}</h3><span className="muted small">{list.length}</span></div>
    <ul className="list">{list.map(u => <UpkeepRow key={u.id} u={u} showPrinter onDone={() => setDone(u)} onEdit={() => setEdit(u)} />)}</ul>
  </section>
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">Upkeep</h2><p className="muted">{due.length ? `${due.length} due now` : 'Nothing due'}{settings?.tasks_upkeep ? ' · due upkeep becomes a task in Pocket Tasks' : ''}</p></div>
        <button type="button" className="btn primary" onClick={() => setEdit('new')} disabled={!printers.length}><Icon name="plus" size={15} />Add</button>
      </div>
      {d.upkeep.length === 0 ? <Empty title="No upkeep yet" icon="wrench">Add what each printer needs every so many print hours or days. Print Farm counts the hours and tells you when it is due.</Empty> : <>
        {group('Due now', due)}
        {group('Coming up', soon)}
        {group('Later', rest)}
      </>}
      <section className="card">
        <div className="card-head"><h3>Log book</h3><span className="spacer" />{spent > 0 && <span className="muted small">{money(spent, cur)} spent</span>}</div>
        {d.log.length === 0 ? <div className="pad muted small">Upkeep done and repairs show here.</div> : <ul className="list">{d.log.slice(0, 40).map(l => (
          <li key={l.id}><div className="list-row compact">
            <div className="grow min0"><div className="ellipsis"><Link to={`/printers/${l.printer_id}`}><b>{l.printer}</b></Link>: {l.title}</div><div className="muted small">{fmtDate(l.done_on)} · at {Math.round(l.hours)} h{l.note ? ` · ${l.note}` : ''}</div></div>
            {l.cost > 0 && <span className="tnum small">{money(l.cost, cur)}</span>}
          </div></li>
        ))}</ul>}
      </section>
      {edit && <UpkeepDialog item={edit === 'new' ? undefined : edit} onClose={() => { setEdit(null); load() }} />}
      {done && <UpkeepDoneDialog printer={printers.find(p => p.id === done.printer_id) || ({ id: done.printer_id, home_ref: '' } as Printer)} item={done} onClose={() => { setDone(null); load() }} />}
    </div>
  )
}
