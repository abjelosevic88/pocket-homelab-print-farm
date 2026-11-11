// Small pieces used on many pages.
import React, { useState } from 'react'
import { Icon } from './icons'
import { PRINT_STATUS, STATE } from '../lib/farm'
import type { PrinterState, PrintStatus, Spool } from '../lib/types'
import { useAppState, openAction } from '../lib/data'
import { useToast, CoreHint } from './ui'
import { api } from '../lib/api'

export function StateBadge({ state, label }: { state: PrinterState; label?: string }) {
  const s = STATE[state] || STATE.idle
  return <span className={`state-badge tone-${s.tone}`}><span className="state-dot" />{label || s.label}</span>
}

export function PrintBadge({ status }: { status: PrintStatus }) {
  const s = PRINT_STATUS[status] || PRINT_STATUS.done
  return <span className={`state-badge tone-${s.tone}`}><Icon name={s.icon} size={12} />{s.label}</span>
}

/** A progress bar that does not move the layout. */
export function Bar({ value, tone = '', thin = false }: { value: number; tone?: string; thin?: boolean }) {
  return <div className={`pbar ${thin ? 'thin' : ''} ${tone}`} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(value * 100)}><span style={{ width: `${Math.max(0, Math.min(1, value)) * 100}%` }} /></div>
}

export function Swatch({ color, size = 14, title }: { color: string; size?: number; title?: string }) {
  return <span className="swatch-dot" title={title} style={{ width: size, height: size, background: color || 'var(--surface-3)' }} />
}

export function SpoolChip({ spool, low = 150 }: { spool: Spool | null; low?: number }) {
  if (!spool) return <span className="spool-chip empty"><Icon name="disc" size={13} />No spool loaded</span>
  return (
    <span className={`spool-chip ${spool.left_g < low ? 'low' : ''}`} title={`${spool.brand} ${spool.material} ${spool.name}`}>
      <Swatch color={spool.color} size={12} />
      <span className="ellipsis">{spool.material} {spool.name || spool.brand}</span>
      <b className="tnum">{Math.round(spool.left_g)} g</b>
    </span>
  )
}

/** A model's picture, or a shape icon. */
export function Cover({ src, size, alt = '', className = '' }: { src?: string | false; size?: number; alt?: string; className?: string }) {
  const [bad, setBad] = useState(false)
  const st: React.CSSProperties = size ? { width: size, height: size } : {}
  return (
    <span className={`cover ${className}`} style={st}>
      {src && !bad ? <img src={src} alt={alt} loading="lazy" onError={() => setBad(true)} /> : <Icon name="package" size={size ? Math.max(16, size * 0.42) : 28} />}
    </span>
  )
}

export function Stars({ value, onChange, size = 18 }: { value: number; onChange?: (v: number) => void; size?: number }) {
  return (
    <span className={`stars-pick ${onChange ? 'edit' : ''}`} aria-label={`${value} of 5`}>
      {[1, 2, 3, 4, 5].map(n => onChange
        ? <button key={n} type="button" className={n <= value ? 'on' : ''} onClick={() => onChange(n === value ? 0 : n)} aria-label={`${n} stars`} style={{ fontSize: size }}>★</button>
        : <span key={n} className={n <= value ? 'on' : ''} style={{ fontSize: size }}>★</span>)}
    </span>
  )
}

export function Temps({ nozzle, nozzleTo, bed, bedTo }: { nozzle: number; nozzleTo: number; bed: number; bedTo: number }) {
  return (
    <span className="temps tnum">
      <span title="Nozzle"><Icon name="thermometer" size={13} />{Math.round(nozzle)}°{nozzleTo > 0 && <small>/{Math.round(nozzleTo)}°</small>}</span>
      <span title="Bed"><Icon name="layers" size={13} />{Math.round(bed)}°{bedTo > 0 && <small>/{Math.round(bedTo)}°</small>}</span>
    </span>
  )
}

/** A button that sends something to another Pocket app — greyed with the reason when that app is not connected. */
export function AppButton({ slug, scope, label, icon, path, body, onDone, className = 'btn sm', confirm }: {
  slug: string; scope?: string; label: string; icon: string; path: string; body?: unknown; onDone?: (r: { url?: string; text?: string }) => void; className?: string; confirm?: () => unknown
}) {
  const state = useAppState()
  const { ok, app, loading } = state(slug, scope)
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const b = confirm ? confirm() : body
      const r = await api.post<{ url?: string; text?: string }>(path, b ?? {})
      toast(r.text || 'Done', 'ok', openAction(r.url))
      onDone?.(r)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const why = !ok && !loading ? (app ? `${app.name}: ${app.detail || app.fix || 'not connected'}` : 'Needs Pocket Core or a direct connection (Settings → Connected apps)') : label
  return <button type="button" className={className} disabled={!ok || busy} title={why} onClick={run}><Icon name={icon} size={14} />{label}</button>
}

export function AppHint({ slug, scope, children }: { slug: string; scope?: string; children?: React.ReactNode }) {
  const state = useAppState()
  const { ok, app, loading } = state(slug, scope)
  if (ok || loading) return null
  return <div className="app-hint muted small"><Icon name="link" size={13} />{children || <>{app?.name || 'That app'} is not connected: {app?.fix || 'see Settings → Connected apps'}.</>}{!app && <CoreHint />}</div>
}

export function InfoGrid({ items }: { items: [React.ReactNode, React.ReactNode][] }) {
  const shown = items.filter(([, v]) => v !== '' && v !== null && v !== undefined && v !== false)
  if (!shown.length) return null
  return <dl className="info-grid">{shown.map(([k, v], i) => <React.Fragment key={i}><dt>{k}</dt><dd>{v}</dd></React.Fragment>)}</dl>
}
