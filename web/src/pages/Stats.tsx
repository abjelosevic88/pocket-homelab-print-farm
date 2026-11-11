// A year of printing in numbers.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { grams, money, reasonLabel } from '../lib/farm'
import { Loading, Segmented } from '../components/ui'
import { Bars, HBars } from '../components/Chart'

type Agg = { name: string; done: number; failed: number; hours: number; grams: number }
type Stats = {
  months: { month: string; done: number; failed: number; hours: number; grams: number; cost: number }[]
  printers: Agg[]; materials: Agg[]; models: Agg[]; reasons: { reason: string; count: number }[]
  total: { done: number; failed: number; hours: number; grams: number; cost: number; success: number }; shelf: Record<string, number>; currency: string
}

const MAT_COLORS: Record<string, string> = { PLA: '#e8641b', PETG: '#2a5db0', ABS: '#8a8d91', ASA: '#6b7280', TPU: '#2f8f5b', PA: '#a16207', PC: '#0e7490' }

export default function StatsPage() {
  const [months, setMonths] = useState(12)
  const [d, setD] = useState<Stats | null>(null)
  const [what, setWhat] = useState<'hours' | 'grams' | 'cost' | 'prints'>('hours')
  useEffect(() => { setD(null); api.get<Stats>(`${V1}/stats?months=${months}`).then(setD).catch(() => {}) }, [months])
  if (!d) return <Loading />
  const name = (m: string) => new Date(Number(m.slice(0, 4)), Number(m.slice(5, 7)) - 1, 1).toLocaleDateString(undefined, { month: 'short' })
  const val = (m: Stats['months'][0]) => what === 'hours' ? m.hours : what === 'grams' ? m.grams / 1000 : what === 'cost' ? m.cost / 100 : m.done + m.failed
  const fmt = (v: number) => what === 'hours' ? `${Math.round(v)} h` : what === 'grams' ? `${v.toFixed(1)} kg` : what === 'cost' ? money(Math.round(v * 100), d.currency) : String(Math.round(v))
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h2 className="page-title">Numbers</h2><p className="muted">The last {months} months</p></div>
        <Segmented size="sm" value={months} onChange={setMonths} options={[{ value: 3, label: '3 months' }, { value: 12, label: 'A year' }, { value: 24, label: 'Two years' }]} />
      </div>
      <div className="stats-row">
        <div className="stat"><div className="stat-l">Printed</div><div className="stat-v tnum">{d.total.done}</div><div className="stat-s small">{d.total.success}% came out</div></div>
        <div className="stat"><div className="stat-l">Hours</div><div className="stat-v tnum">{Math.round(d.total.hours)}</div><div className="stat-s small">of printing</div></div>
        <div className="stat"><div className="stat-l">Filament</div><div className="stat-v tnum">{grams(d.total.grams)}</div></div>
        <div className="stat"><div className="stat-l">It cost</div><div className="stat-v tnum">{money(d.total.cost, d.currency)}</div><div className="stat-s small">filament, power, wear</div></div>
      </div>
      <section className="card">
        <div className="card-head"><h3>By month</h3><span className="spacer" /><Segmented size="sm" value={what} onChange={setWhat} options={[{ value: 'hours', label: 'Hours' }, { value: 'prints', label: 'Prints' }, { value: 'grams', label: 'Filament' }, { value: 'cost', label: 'Cost' }]} /></div>
        <div className="pad"><Bars items={d.months.map(m => ({ label: name(m.month), value: val(m), hint: `${m.done} done, ${m.failed} failed` }))} format={fmt} height={170} /></div>
      </section>
      <div className="cols2 even">
        <section className="card">
          <div className="card-head"><h3>Printers</h3></div>
          <div className="pad"><HBars items={d.printers.map(p => ({ label: p.name, value: p.hours, hint: `${p.done} done, ${p.failed} failed (${p.done + p.failed ? Math.round(p.done / (p.done + p.failed) * 100) : 0}% good)` }))} format={v => `${Math.round(v)} h`} /></div>
        </section>
        <section className="card">
          <div className="card-head"><h3>Materials used</h3></div>
          <div className="pad"><HBars items={d.materials.map(m => ({ label: m.name, value: m.grams, color: MAT_COLORS[m.name] }))} format={v => grams(v)} /></div>
        </section>
        <section className="card">
          <div className="card-head"><h3>Why prints failed</h3></div>
          <div className="pad">{d.reasons.length ? <HBars items={d.reasons.map(r => ({ label: reasonLabel(r.reason), value: r.count, color: 'var(--danger)' }))} format={v => String(v)} /> : <span className="muted small">Nothing failed. 🎉</span>}</div>
        </section>
        <section className="card">
          <div className="card-head"><h3>Printed most</h3></div>
          <div className="pad"><HBars items={d.models.map(m => ({ label: m.name, value: m.done + m.failed, hint: `${m.failed} failed` }))} format={v => `${v}×`} /></div>
        </section>
      </div>
      {Object.keys(d.shelf).length > 0 && <section className="card">
        <div className="card-head"><h3>On the shelf now</h3></div>
        <div className="pad"><HBars items={Object.entries(d.shelf).sort((a, b) => b[1] - a[1]).map(([k, v]) => ({ label: k, value: v, color: MAT_COLORS[k] }))} format={v => grams(v)} /></div>
      </section>}
    </div>
  )
}
