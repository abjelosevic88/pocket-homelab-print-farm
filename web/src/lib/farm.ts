// Words and numbers for the farm.
import type { PrinterState, PrintStatus } from './types'
import { fmtTime } from './format'

export const STATE: Record<PrinterState, { label: string; tone: string; icon: string }> = {
  idle: { label: 'Ready', tone: 'ok', icon: 'check-circle' },
  printing: { label: 'Printing', tone: 'busy', icon: 'printer' },
  paused: { label: 'Paused', tone: 'warn', icon: 'pause' },
  done: { label: 'Done — clear the bed', tone: 'done', icon: 'check' },
  stopped: { label: 'Stopped', tone: 'warn', icon: 'stop' },
  error: { label: 'Needs you', tone: 'bad', icon: 'alert' },
  busy: { label: 'Busy', tone: 'busy', icon: 'timer' },
  offline: { label: 'Not answering', tone: 'off', icon: 'x-circle' },
  manual: { label: 'Ready', tone: 'ok', icon: 'check-circle' },
}

export const PRINT_STATUS: Record<PrintStatus, { label: string; tone: string; icon: string }> = {
  printing: { label: 'Printing', tone: 'busy', icon: 'printer' },
  paused: { label: 'Paused', tone: 'warn', icon: 'pause' },
  done: { label: 'Done', tone: 'ok', icon: 'check-circle' },
  failed: { label: 'Failed', tone: 'bad', icon: 'x-circle' },
  cancelled: { label: 'Stopped', tone: 'off', icon: 'stop' },
}

export const REASONS: { value: string; label: string }[] = [
  { value: 'adhesion', label: 'Came off the bed' }, { value: 'spaghetti', label: 'Spaghetti' }, { value: 'warping', label: 'Warping' },
  { value: 'clog', label: 'Clogged nozzle' }, { value: 'under_extrusion', label: 'Under-extrusion' }, { value: 'layer_shift', label: 'Layer shift' },
  { value: 'stringing', label: 'Stringing' }, { value: 'supports', label: 'Supports failed' }, { value: 'filament_out', label: 'Ran out of filament' },
  { value: 'power', label: 'Power cut' }, { value: 'cancelled_by_me', label: 'I stopped it' }, { value: 'other', label: 'Something else' },
]
export const reasonLabel = (v: string) => REASONS.find(r => r.value === v)?.label || v.replace(/_/g, ' ')

export const CONNECTIONS = [
  { value: 'manual', label: 'By hand', hint: 'You say when a print starts and ends' },
  { value: 'moonraker', label: 'Klipper (Moonraker)', hint: 'Voron, Sovol, Elegoo, Creality K1… with Mainsail or Fluidd' },
  { value: 'octoprint', label: 'OctoPrint', hint: 'Any printer with an OctoPrint Raspberry Pi' },
  { value: 'prusalink', label: 'PrusaLink', hint: 'Prusa MK4, MK3.9, XL, MINI, CORE One' },
] as const

export const MATERIALS = ['PLA', 'PETG', 'ABS', 'ASA', 'TPU', 'PA', 'PC', 'PLA-CF', 'PETG-CF', 'PVA', 'HIPS', 'RESIN']

/** 2 h 05 min, 45 min. */
export function hm(sec: number): string {
  if (!sec || sec < 0) return '—'
  if (sec < 60) return `${Math.round(sec)} s`
  const h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60
  return h ? `${h} h ${String(m).padStart(2, '0')} min` : `${m} min`
}

/** Short: 2:05 h, 45 min. */
export function hmShort(sec: number): string {
  if (!sec || sec < 0) return '—'
  const h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60
  return h ? `${h}:${String(m).padStart(2, '0')} h` : `${m} min`
}

/** The clock time something ends (today, tomorrow, or a weekday). */
export function endsAt(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso), now = new Date()
  const t = fmtTime(d)
  const days = Math.round((new Date(d.toDateString()).getTime() - new Date(now.toDateString()).getTime()) / 86400000)
  if (days === 0) return t
  if (days === 1) return `tomorrow ${t}`
  return `${d.toLocaleDateString(undefined, { weekday: 'short' })} ${t}`
}

export function money(minor: number, cur: string): string {
  const v = (minor || 0) / 100
  try { return new Intl.NumberFormat(undefined, { style: 'currency', currency: cur || 'EUR', maximumFractionDigits: 2 }).format(v) } catch { return `${v.toFixed(2)} ${cur}` }
}

export function grams(g: number): string {
  if (!g) return '0 g'
  if (g >= 1000) return `${(g / 1000).toFixed(g >= 10000 ? 0 : 2)} kg`
  return `${Math.round(g)} g`
}

export function mm(v: number): string { return v ? `${Math.round(v)}` : '—' }

export function sizeOf(x: number, y: number, z: number): string {
  if (!x && !y && !z) return ''
  return `${mm(x)} × ${mm(y)} × ${mm(z)} mm`
}

/** A spool's colour as a CSS background (a ring for white). */
export function swatchStyle(color: string): React.CSSProperties {
  return { background: color || 'var(--surface-3)' }
}

/** The settings people compare, with their labels, in this order. */
export const SETTING_LABELS: [string, string][] = [
  ['layer_height', 'Layer height'], ['walls', 'Walls'], ['infill', 'Infill'], ['infill_type', 'Infill pattern'], ['supports', 'Supports'], ['brim', 'Brim'],
  ['nozzle_temp', 'Nozzle °C'], ['bed_temp', 'Bed °C'], ['speed', 'Speed'], ['profile', 'Profile'], ['filament', 'Filament profile'],
]
export const settingLabel = (k: string) => SETTING_LABELS.find(s => s[0] === k)?.[1] || k.replace(/_/g, ' ')

export function sortedSettings(s: Record<string, string> | null | undefined): [string, string][] {
  if (!s) return []
  const order = SETTING_LABELS.map(x => x[0])
  return Object.entries(s).filter(([, v]) => v !== '').sort((a, b) => (order.indexOf(a[0]) + 1 || 99) - (order.indexOf(b[0]) + 1 || 99))
}

export const kindLabel = (k: string) => ({ stl: 'STL', '3mf': '3MF', obj: 'OBJ', step: 'STEP', gcode: 'G-code', bgcode: 'Binary G-code' } as Record<string, string>)[k] || k.toUpperCase()
