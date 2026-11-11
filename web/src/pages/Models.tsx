// The model library and one model: its 3D view, files, what the slicer
// said, the settings that worked and every print of it.
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, uploadBlob, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useAppState, useOnChange } from '../lib/data'
import { grams, hm, kindLabel, settingLabel, sizeOf, sortedSettings } from '../lib/farm'
import type { Job, Model, ModelFile, Print, Printer } from '../lib/types'
import { ago, fmtBytes, fmtDate } from '../lib/format'
import { Confirm, Empty, Field, Loading, MenuButton, Modal, Segmented, useDebounce, useToast, errMsg, type MenuItem } from '../components/ui'
import { Select } from '../components/Select'
import { Icon } from '../components/icons'
import { AppButton, Cover, InfoGrid, PrintBadge, Stars } from '../components/bits'
import { AddModelsDialog, JobDialog, RecordDialog, StartDialog } from '../components/dialogs'
import { Viewer3D } from '../components/Viewer3D'
import { useCrumb } from '../components/Layout'

export default function Models() {
  const navigate = useNavigate()
  const [sp, setSp] = useSearchParams()
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 250)
  const [tag, setTag] = useState(sp.get('tag') || '')
  const [sort, setSort] = useState<'added' | 'name' | 'printed'>('added')
  const [fav, setFav] = useState(sp.get('filter') === 'fav')
  const [list, setList] = useState<{ models: Model[]; total: number } | null>(null)
  const [tags, setTags] = useState<{ tag: string; count: number }[]>([])
  const [adding, setAdding] = useState(sp.get('add') === '1')
  const load = useCallback(() => {
    const p = new URLSearchParams({ q: dq, tag, sort, limit: '500' })
    if (fav) p.set('fav', '1')
    api.get<{ models: Model[]; total: number }>(`${V1}/models?${p}`).then(setList).catch(() => setList({ models: [], total: 0 }))
    api.get<{ tags: { tag: string; count: number }[] }>(`${V1}/models/tags`).then(r => setTags(r.tags)).catch(() => {})
  }, [dq, tag, sort, fav])
  useEffect(load, [load])
  useOnChange(load)
  useEffect(() => { if (sp.get('add') === '1') { setAdding(true); sp.delete('add'); setSp(sp, { replace: true }) } }, [sp, setSp])
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">Models</h2><p className="muted">{list ? `${list.total} in the library` : ' '}</p></div>
        <button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="upload" size={15} />Add models</button>
      </div>
      <div className="toolbar row wrap">
        <div className="search-box grow"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Find a model, a tag, a file…" aria-label="Find" /></div>
        <Segmented size="sm" value={sort} onChange={setSort} options={[{ value: 'added', label: 'New' }, { value: 'printed', label: 'Printed' }, { value: 'name', label: 'A–Z' }]} />
        <button type="button" className={`btn sm ${fav ? 'primary' : ''}`} onClick={() => setFav(!fav)} title="Favourites"><Icon name="star" size={14} /></button>
      </div>
      {tags.length > 0 && <div className="chips">
        {tags.slice(0, 18).map(t => <button key={t.tag} type="button" className={`chip ${tag === t.tag ? 'on' : ''}`} onClick={() => setTag(tag === t.tag ? '' : t.tag)}>{t.tag} <span className="muted">{t.count}</span></button>)}
      </div>}
      {!list ? <Loading /> : list.models.length === 0 ? <Empty title={q || tag || fav ? 'Nothing found' : 'No models yet'} icon="package">
        {!(q || tag || fav) && <>Add STL, 3MF or OBJ files to see them in 3D, and the sliced G-code to send to a printer. <div className="mt"><button type="button" className="btn primary" onClick={() => setAdding(true)}><Icon name="upload" size={15} />Add models</button></div></>}
      </Empty> : (
        <div className="model-grid">
          {list.models.map(m => (
            <Link key={m.id} to={`/models/${m.id}`} className="model-card">
              <Cover src={m.has_cover ? `${V1}/models/${m.id}/cover` : false} className="model-cover" />
              {m.favourite && <Icon name="star" size={14} className="model-fav" />}
              <div className="model-info">
                <b className="ellipsis">{m.name}</b>
                <span className="muted small ellipsis">{[m.est_seconds ? hm(m.est_seconds) : '', m.est_grams ? grams(m.est_grams) : '', m.prints ? `${m.prints}× printed` : 'not printed yet'].filter(Boolean).join(' · ')}</span>
                <span className="kinds">{m.kinds.split(',').filter(Boolean).map(k => <span key={k} className={`kind k-${k}`}>{kindLabel(k)}</span>)}</span>
              </div>
            </Link>
          ))}
        </div>
      )}
      {adding && <AddModelsDialog onClose={() => setAdding(false)} onAdded={ms => { if (ms.length === 1) navigate(`/models/${ms[0].id}`); else load() }} />}
    </div>
  )
}

type ModelRes = { model: Model; files: ModelFile[]; prints: Print[]; kept: Print[]; queued: Job[] }

export function ModelPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const { toast } = useToast()
  const state = useAppState()
  const [d, setD] = useState<ModelRes | null>(null)
  const [shown, setShown] = useState<number>(0)
  const [edit, setEdit] = useState(false)
  const [del, setDel] = useState(false)
  const [adding, setAdding] = useState(false)
  const [queue, setQueue] = useState(false)
  const [start, setStart] = useState(false)
  const [record, setRecord] = useState(false)
  const [ai, setAi] = useState(false)
  const load = useCallback(() => {
    api.get<ModelRes>(`${V1}/models/${id}`).then(r => { setD(r); setShown(s => s && r.files.some(f => f.id === s) ? s : (r.files.find(f => ['stl', '3mf', 'obj'].includes(f.kind))?.id || 0)) }).catch(e => { toast(errMsg(e), 'err'); navigate('/models') })
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load])
  useOnChange(load)
  useCrumb(d?.model.name, '/models')
  if (!d) return <Loading />
  const m = d.model
  const meshFile = d.files.find(f => f.id === shown)
  const gcode = d.files.filter(f => f.printable)
  const fav = async () => { await api.put(`${V1}/models/${m.id}`, { favourite: !m.favourite }).catch(() => {}); load() }
  const snapshot = async (b: Blob) => {
    try { await uploadBlob(`${V1}/models/${m.id}/cover`, b, 'cover.png'); toast('That view is the model’s picture now'); load(); announceChange() } catch (e) { toast(errMsg(e), 'err') }
  }
  const removeFile = async (f: ModelFile) => { if (!confirm(`Remove ${f.name}?`)) return; try { await api.del(`${V1}/files/${f.id}`); load() } catch (e) { toast(errMsg(e), 'err') } }
  const remove = async () => { try { await api.del(`${V1}/models/${m.id}`); announceChange(); navigate('/models') } catch (e) { toast(errMsg(e), 'err') } }
  const filesOn = state('files', 'files:write').ok
  const good = d.prints.filter(p => p.status === 'done').length, bad = d.prints.filter(p => p.status === 'failed').length
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow min0">
          <h2 className="page-title row">{m.name}<button type="button" className={`btn ghost icon-only sm fav ${m.favourite ? 'on' : ''}`} onClick={fav} aria-label="Favourite" title="Favourite"><Icon name="star" size={16} /></button></h2>
          <p className="muted">{[m.designer && `by ${m.designer}`, m.license, `added ${ago(m.created_at)}`].filter(Boolean).join(' · ')}{m.url && <> · <a href={m.url} target="_blank" rel="noreferrer">where it came from <Icon name="external" size={12} /></a></>}</p>
        </div>
        <div className="row wrap">
          <button type="button" className="btn" onClick={() => setQueue(true)}><Icon name="list-plus" size={15} /><span className="hide-sm">To the queue</span></button>
          <button type="button" className="btn primary" onClick={() => setStart(true)}><Icon name="play" size={15} />Print now</button>
          <MenuButton label="More" align="right" items={(): MenuItem[] => [
            { label: 'Edit', icon: 'edit', onSelect: () => setEdit(true) },
            { label: 'Add files', icon: 'upload', onSelect: () => setAdding(true) },
            { label: 'Write down a print', icon: 'history', onSelect: () => setRecord(true) },
            { label: 'Suggest settings (AI)', icon: 'sparkles', disabled: !settings?.ai_help, onSelect: () => setAi(true) },
            'sep',
            { label: 'Remove the model', icon: 'trash', danger: true, onSelect: () => setDel(true) },
          ]}><Icon name="more" size={16} /></MenuButton>
        </div>
      </div>
      <div className="cols2 model-cols">
        <div className="stack">
          <section className="card flush viewer-card">
            {meshFile ? <Viewer3D url={`${V1}/files/${meshFile.id}/mesh`} color={settings?.demo ? '#e8641b' : '#e8641b'} onSnapshot={snapshot} />
              : <div className="viewer"><div className="viewer-msg muted"><Cover src={m.has_cover ? `${V1}/models/${m.id}/cover` : false} size={160} />{gcode.length ? 'Only G-code: add the STL or 3MF to turn it in 3D.' : 'No shape yet.'}</div></div>}
          </section>
          {m.description && <p className="pre">{m.description}</p>}
          {m.tags && <div className="chips">{m.tags.split(',').map(t => t.trim()).filter(Boolean).map(t => <Link key={t} to={`/models?tag=${encodeURIComponent(t)}`} className="chip">{t}</Link>)}</div>}
          <section className="card">
            <div className="card-head"><h3>Files</h3><span className="spacer" /><button type="button" className="btn sm" onClick={() => setAdding(true)}><Icon name="plus" size={13} />Add</button></div>
            <ul className="list">
              {d.files.map(f => (
                <li key={f.id}><div className={`list-row compact file-row ${f.id === shown ? 'cur' : ''}`}>
                  <span className={`kind k-${f.kind}`}>{kindLabel(f.kind)}</span>
                  <button type="button" className="grow min0 linkish plain" disabled={!['stl', '3mf', 'obj'].includes(f.kind)} onClick={() => setShown(f.id)}>
                    <div className="ellipsis">{f.name}</div>
                    <div className="muted small ellipsis">{[fmtBytes(f.size), sizeOf(f.size_x, f.size_y, f.size_z), f.est_seconds ? hm(f.est_seconds) : '', f.est_grams ? grams(f.est_grams) : '', f.layer_height ? `${f.layer_height} mm layers` : '', f.material, f.slicer].filter(Boolean).join(' · ')}</div>
                  </button>
                  <MenuButton label="File" align="right" items={(): MenuItem[] => [
                    { label: 'Download', icon: 'download', onSelect: () => { window.location.href = `${V1}/files/${f.id}/download` } },
                    { label: 'Copy to Pocket Files', icon: 'archive', disabled: !filesOn, hint: filesOn ? undefined : 'Needs Pocket Files', onSelect: async () => { try { const r = await api.post<{ text: string }>(`${V1}/files/${f.id}/files`, {}); toast(r.text) } catch (e) { toast(errMsg(e), 'err') } } },
                    { label: 'Remove', icon: 'trash', danger: true, onSelect: () => removeFile(f) },
                  ]}><Icon name="more" size={15} /></MenuButton>
                </div></li>
              ))}
            </ul>
          </section>
        </div>
        <div className="stack">
          <section className="card">
            <div className="card-head"><h3>What the slicer said</h3></div>
            <div className="pad">
              {gcode.length ? gcode.slice(-1).map(f => <div key={f.id}>
                <InfoGrid items={[['Time', hm(f.est_seconds)], ['Filament', f.est_grams ? `${grams(f.est_grams)} (${(f.est_mm / 1000).toFixed(1)} m)` : ''], ['Size', sizeOf(f.size_x, f.size_y, f.size_z)], ['Material', f.material], ['Nozzle', f.nozzle ? `${f.nozzle} mm` : ''], ['Layers', f.layer_height ? `${f.layer_height} mm` : ''], ['For', f.printer_model], ['Slicer', f.slicer]]} />
                {sortedSettings(f.settings).length > 0 && <InfoGrid items={sortedSettings(f.settings).filter(([k]) => k !== 'layer_height').map(([k, v]) => [settingLabel(k), v])} />}
              </div>) : <div className="muted small">Add the sliced G-code (from PrusaSlicer, OrcaSlicer, Bambu Studio or Cura): its time, grams and settings show here, and it can go straight to a connected printer.<br />{m.size_x > 0 && <>Its size: {sizeOf(m.size_x, m.size_y, m.size_z)}.</>}</div>}
            </div>
          </section>
          {d.kept.length > 0 && <section className="card">
            <div className="card-head"><h3><Icon name="check-circle" size={15} className="ok" /> Settings that worked</h3></div>
            <ul className="list">{d.kept.map(p => (
              <li key={p.id}><Link to={`/prints/${p.id}`} className="list-row compact">
                <div className="grow min0"><div className="small">{fmtDate(p.started_at.slice(0, 10))} · {p.printer} · {p.spool || p.material}</div>
                  <div className="settings-line small">{sortedSettings(p.settings).map(([k, v]) => <span key={k}><b>{settingLabel(k)}</b> {v}</span>)}</div></div>
              </Link></li>
            ))}</ul>
          </section>}
          {d.queued.length > 0 && <section className="card">
            <div className="card-head"><h3>In the queue</h3></div>
            <ul className="list">{d.queued.map(j => <li key={j.id}><Link to="/queue" className="list-row compact"><span className="grow">{j.title}{j.for_whom && <span className="muted"> · for {j.for_whom}</span>}</span><span className="muted small tnum">{j.done_count}/{j.quantity}</span></Link></li>)}</ul>
          </section>}
          <section className="card">
            <div className="card-head"><h3>Prints</h3><span className="spacer" /><span className="muted small">{good} done{bad ? ` · ${bad} failed` : ''}</span></div>
            {d.prints.length === 0 ? <div className="pad muted small">Not printed yet.</div> : <ul className="list">{d.prints.slice(0, 12).map(p => (
              <li key={p.id}><Link to={`/prints/${p.id}`} className="list-row compact">
                <div className="grow min0"><div className="ellipsis small">{fmtDate(p.started_at.slice(0, 10))} · {p.printer || '—'}{p.copies > 1 ? ` · ${p.copies} pieces` : ''}</div><div className="muted small">{hm(p.seconds)}{p.grams ? ` · ${Math.round(p.grams)} g` : ''}{p.rating ? ' · ' : ''}{p.rating > 0 && <Stars value={p.rating} size={11} />}</div></div>
                <PrintBadge status={p.status} />
              </Link></li>
            ))}</ul>}
          </section>
        </div>
      </div>
      {edit && <ModelEdit model={m} onClose={() => { setEdit(false); load() }} />}
      {del && <Confirm title={`Remove ${m.name}?`} message="Its files are deleted from this server. Its prints stay in the history." confirmLabel="Remove" onCancel={() => setDel(false)} onConfirm={remove} />}
      {adding && <AddModelsDialog modelId={m.id} onClose={() => { setAdding(false); load() }} />}
      {queue && <JobDialog modelId={m.id} onClose={() => { setQueue(false); load() }} />}
      {start && <StartDialog model={m} onClose={() => setStart(false)} onStarted={() => navigate('/')} />}
      {record && <RecordDialog modelId={m.id} onClose={() => { setRecord(false); load() }} />}
      {ai && <AISettings model={m} onClose={() => setAi(false)} />}
    </div>
  )
}

function ModelEdit({ model, onClose }: { model: Model; onClose: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: model.name, description: model.description, tags: model.tags, designer: model.designer, license: model.license, url: model.url })
  const save = async () => { try { await api.put(`${V1}/models/${model.id}`, f); toast('Saved'); announceChange(); onClose() } catch (e) { toast(errMsg(e), 'err') } }
  return (
    <Modal title="Edit the model" onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={save}>Save</button></>}>
      <div className="stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="About it"><textarea rows={3} value={f.description} onChange={e => setF({ ...f, description: e.target.value })} placeholder="How to print it, what it is for…" /></Field>
        <Field label="Tags" help="Separated by commas."><input value={f.tags} onChange={e => setF({ ...f, tags: e.target.value })} placeholder="home, gift, spare part" /></Field>
        <div className="grid two">
          <Field label="Designer"><input value={f.designer} onChange={e => setF({ ...f, designer: e.target.value })} /></Field>
          <Field label="Licence"><input value={f.license} onChange={e => setF({ ...f, license: e.target.value })} placeholder="CC BY 4.0" /></Field>
        </div>
        <Field label="Where it came from"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://www.printables.com/model/…" /></Field>
      </div>
    </Modal>
  )
}

function AISettings({ model, onClose }: { model: Model; onClose: () => void }) {
  const { toast } = useToast()
  const [printers, setPrinters] = useState<Printer[]>([])
  const [printer, setPrinter] = useState('')
  const [material, setMaterial] = useState('')
  const [ask, setAsk] = useState('')
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<{ summary: string; settings: Record<string, string>; why: string[] } | null>(null)
  useEffect(() => { api.get<{ printers: Printer[] }>(`${V1}/printers`).then(r => setPrinters(r.printers)).catch(() => {}) }, [])
  const run = async () => {
    setBusy(true)
    try { setRes(await api.post(`${V1}/models/${model.id}/settings`, { printer_id: Number(printer) || 0, material, ask })) } catch (e) { toast(errMsg(e), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Which settings to start from" onClose={onClose} footer={<><button type="button" className="btn" onClick={onClose}>Close</button><button type="button" className="btn primary" onClick={run} disabled={busy}><Icon name="sparkles" size={15} />{busy ? 'Thinking…' : res ? 'Ask again' : 'Ask'}</button></>}>
      <div className="stack">
        <p className="muted small">Pocket AI looks at your own prints of this model and the settings that worked on others, and suggests where to start. Nothing changes by itself.</p>
        <div className="grid two">
          <Field label="On"><Select ariaLabel="Printer" value={printer} onChange={setPrinter} options={printers.map(p => ({ value: String(p.id), label: p.name }))} placeholder="Any printer" clearLabel="Any printer" /></Field>
          <Field label="Material"><input value={material} onChange={e => setMaterial(e.target.value.toUpperCase())} placeholder="PETG" /></Field>
        </div>
        <Field label="Anything else"><input value={ask} onChange={e => setAsk(e.target.value)} placeholder="It has to be strong; it goes outside" /></Field>
        {res && <div className="ai-box">
          <p><b>{res.summary}</b></p>
          <InfoGrid items={Object.entries(res.settings).map(([k, v]) => [settingLabel(k), v])} />
          {res.why?.length > 0 && <ul className="small">{res.why.map((w, i) => <li key={i}>{w}</li>)}</ul>}
        </div>}
      </div>
    </Modal>
  )
}

