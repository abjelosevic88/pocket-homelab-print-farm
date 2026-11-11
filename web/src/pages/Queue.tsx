// The queue: what to print next, in order, how many, for whom.
import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useAppState, useOnChange, openAction } from '../lib/data'
import { hm, money } from '../lib/farm'
import type { Job } from '../lib/types'
import { fmtDate, todayISO } from '../lib/format'
import { Empty, Field, Loading, MenuButton, Modal, Segmented, useToast, errMsg, type MenuItem } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { Bar, Cover, Swatch } from '../components/bits'
import { JobDialog, MoneyInput, StartDialog } from '../components/dialogs'

export default function Queue() {
  const { settings } = useApp()
  const { toast } = useToast()
  const state = useAppState()
  const [view, setView] = useState<'open' | 'done'>('open')
  const [jobs, setJobs] = useState<Job[] | null>(null)
  const [sp, setSp] = useSearchParams()
  const [edit, setEdit] = useState<Job | 'new' | null>(sp.get('new') === '1' ? 'new' : null)
  useEffect(() => { if (sp.get('new') === '1') { setEdit('new'); sp.delete('new'); setSp(sp, { replace: true }) } }, [sp, setSp])
  const [start, setStart] = useState<Job | null>(null)
  const [invoice, setInvoice] = useState<Job | null>(null)
  const load = useCallback(() => {
    api.get<{ jobs: Job[] }>(`${V1}/jobs?status=${view === 'open' ? 'open' : 'done,cancelled'}`).then(r => setJobs(r.jobs)).catch(() => setJobs([]))
  }, [view])
  useEffect(load, [load])
  useOnChange(load)
  const cur = settings?.currency || 'EUR'
  const move = async (i: number, d: number) => {
    if (!jobs) return
    const ids = jobs.filter(j => j.status === 'queued').map(j => j.id)
    const id = jobs[i].id
    const k = ids.indexOf(id), j = k + d
    if (k < 0 || j < 0 || j >= ids.length) return;
    [ids[k], ids[j]] = [ids[j], ids[k]]
    await api.put(`${V1}/jobs/order`, { ids }).catch(() => {})
    load()
  }
  const set = async (j: Job, body: Partial<Job>, msg: string) => {
    try { await api.put(`${V1}/jobs/${j.id}`, body); toast(msg); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const remove = async (j: Job) => { try { await api.del(`${V1}/jobs/${j.id}`); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') } }
  const task = async (j: Job) => {
    try { const r = await api.post<{ url?: string; text?: string }>(`${V1}/jobs/${j.id}/task`, {}); toast(r.text || 'Done', 'ok', openAction(r.url)); load() } catch (e) { toast(errMsg(e), 'err') }
  }
  const tasksOn = state('tasks', 'tasks:write').ok
  const invOn = state('invoicing', 'invoices:write').ok
  const today = todayISO()
  const totalLeft = (jobs || []).filter(j => j.status !== 'done').reduce((a, j) => a + j.est_seconds * Math.max(0, j.quantity - j.done_count), 0)
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">The queue</h2><p className="muted">{view === 'open' && jobs ? `${jobs.length} ${jobs.length === 1 ? 'job' : 'jobs'}${totalLeft ? ` · about ${hm(totalLeft)} of printing` : ''}` : 'Finished and cancelled jobs'}</p></div>
        <Segmented size="sm" value={view} onChange={setView} options={[{ value: 'open', label: 'To print' }, { value: 'done', label: 'Done' }]} />
        <button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add</button>
      </div>
      {!jobs ? <Loading /> : jobs.length === 0 ? <Empty title={view === 'open' ? 'Nothing in the queue' : 'Nothing done yet'} icon="list">
        {view === 'open' && <>Add what you want to print next: a model from the library, how many, which colour, for whom and by when.<div className="mt"><button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add a job</button></div></>}
      </Empty> : (
        <div className="card flush">
          <ul className="list">
            {jobs.map((j, i) => {
              const late = j.due_on && j.due_on < today && j.status === 'queued'
              const menu = (): MenuItem[] => [
                { label: 'Edit', icon: 'edit', onSelect: () => setEdit(j) },
                ...(j.status === 'queued' ? [{ label: 'Done (all printed)', icon: 'check', onSelect: () => set(j, { status: 'done', done_count: j.quantity }, 'Done') }, { label: 'Cancel the job', icon: 'x', onSelect: () => set(j, { status: 'cancelled' }, 'Cancelled') }] : [{ label: 'Back in the queue', icon: 'undo', onSelect: () => set(j, { status: 'queued' }, 'Back in the queue') }]),
                'sep',
                { label: j.task_ref ? 'In Pocket Tasks' : 'Make a task', icon: 'check-square', disabled: !tasksOn || !!j.task_ref, hint: tasksOn ? undefined : 'Needs Pocket Tasks', onSelect: () => task(j) },
                { label: j.invoice_ref ? 'Invoice made' : 'Make an invoice', icon: 'receipt', disabled: !invOn, hint: invOn ? undefined : 'Needs Pocket Invoicing', onSelect: () => j.invoice_ref ? window.open(j.invoice_ref, '_blank') : setInvoice(j) },
                'sep',
                { label: 'Remove', icon: 'trash', danger: true, onSelect: () => remove(j) },
              ]
              return (
                <li key={j.id}>
                  <div className="list-row job-row">
                    {view === 'open' && j.status === 'queued' && <div className="mover hide-sm">
                      <button type="button" className="btn ghost icon-only xs" aria-label="Earlier" onClick={() => move(i, -1)}><Icon name="chevron-up" size={13} /></button>
                      <button type="button" className="btn ghost icon-only xs" aria-label="Later" onClick={() => move(i, 1)}><Icon name="chevron-down" size={13} /></button>
                    </div>}
                    <Cover src={j.has_cover && j.model_id ? `${V1}/models/${j.model_id}/cover` : false} size={48} />
                    <div className="grow min0">
                      <div className="ellipsis">{j.model_id ? <Link to={`/models/${j.model_id}`}><b>{j.title}</b></Link> : <b>{j.title}</b>}{j.for_whom && <span className="muted"> · for {j.for_whom}</span>}</div>
                      <div className="muted small ellipsis">{[j.printer || 'any printer', j.filament || j.material, j.est_seconds ? `${hm(j.est_seconds)} each` : 'no G-code', j.due_on && (late ? `⚠ was due ${fmtDate(j.due_on)}` : `by ${fmtDate(j.due_on)}`), j.price ? `${money(j.price, cur)} each` : ''].filter(Boolean).join(' · ')}</div>
                      {j.quantity > 1 && <div className="job-count"><Bar value={j.done_count / j.quantity} thin /><span className="small tnum">{j.done_count}/{j.quantity}{j.printing > 0 ? ` · ${j.printing} printing` : ''}</span></div>}
                    </div>
                    {j.color && <Swatch color={j.color} title={j.filament} />}
                    {j.status === 'printing' ? <span className="badge accent">printing</span> : j.status === 'queued' ? <button type="button" className="btn sm" onClick={() => setStart(j)}><Icon name="play" size={13} /><span className="hide-sm">Start</span></button> : <span className="badge">{j.status === 'done' ? 'done' : 'cancelled'}</span>}
                    <MenuButton label="More" items={menu} align="right"><Icon name="more" size={16} /></MenuButton>
                  </div>
                </li>
              )
            })}
          </ul>
        </div>
      )}
      {edit && <JobDialog job={edit === 'new' ? undefined : edit} onClose={() => { setEdit(null); load() }} />}
      {start && <StartDialog job={start} onClose={() => setStart(null)} onStarted={load} />}
      {invoice && <InvoiceDialog job={invoice} onClose={() => { setInvoice(null); load() }} />}
    </div>
  )
}

function InvoiceDialog({ job, onClose }: { job: Job; onClose: () => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [clients, setClients] = useState<{ id: string; name: string }[] | null>(null)
  const [client, setClient] = useState('')
  const [price, setPrice] = useState(job.price)
  useEffect(() => { api.get<{ clients: { id: string; name: string }[] }>(`${V1}/apps/invoicing/clients`).then(r => setClients(r.clients)).catch(e => toast(errMsg(e), 'err')) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (clients && job.for_whom) { const c = clients.find(x => x.name.toLowerCase() === job.for_whom.toLowerCase()); if (c) setClient(c.id) } }, [clients, job.for_whom])
  const make = async () => {
    try { const r = await api.post<{ url?: string; text?: string }>(`${V1}/jobs/${job.id}/invoice`, { client_id: Number(client), price }); toast(r.text || 'Done', 'ok', openAction(r.url)); onClose() } catch (e) { toast(errMsg(e), 'err') }
  }
  const qty = job.done_count || job.quantity
  return (
    <Modal title="Make a draft invoice" onClose={onClose} size="sm" footer={<>
      <button type="button" className="btn" onClick={onClose}>Cancel</button>
      <button type="button" className="btn primary" onClick={make} disabled={!client || !price}>Make it</button>
    </>}>
      <div className="stack">
        <Field label="For"><Select ariaLabel="Client" value={client} onChange={setClient} options={(clients || []).map(c => ({ value: c.id, label: c.name }))} placeholder={clients ? 'Choose the client' : 'Loading…'} /></Field>
        <Field label="Price each"><MoneyInput value={price} onChange={setPrice} currency={settings?.currency} /></Field>
        <p className="muted small">{qty} × {job.title} = {money(price * qty, settings?.currency || 'EUR')}. It lands in Pocket Invoicing as a draft to check and send.</p>
      </div>
    </Modal>
  )
}
