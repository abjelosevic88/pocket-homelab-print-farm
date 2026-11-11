// The shapes the API sends.

export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = 'core' | 'direct' | ''
export type AppSlug = 'files' | 'inventory' | 'money' | 'invoicing' | 'contacts' | 'tasks' | 'calendar' | 'chat' | 'parcels' | 'projects' | 'home' | 'alerts'
type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  currency: string; kwh_price: number; markup: number
  low_grams: number; auto_deduct: boolean; default_material: string
  need_clear: boolean; auto_start: boolean; send_gcode: boolean; default_fail: 'failed' | 'cancelled'
  files_folder: string; calendar_long: number; tasks_upkeep: boolean; tasks_list: string; inventory_location: string
  money_category: string; money_account: string; money_on_spool: boolean; chat_to: string; ai_help: boolean
  notify_done: boolean; notify_failed: boolean; notify_low: boolean; notify_upkeep: boolean; notify_offline: boolean; notify_hour: number; poll_seconds: number
  notify_url: string; notify_format: string; notify_configured: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; demo: boolean
}

export type AppStateName = 'on' | 'partly' | 'off' | 'link_off' | 'missing' | 'old' | 'no_core' | 'not_set_up' | 'error'
export interface AppState { slug: AppSlug; name: string; reason: string; mode: ConnMode; via: ConnVia; state: AppStateName; detail: string; fix: string; on: string[]; open_url: string }

export type Connection = 'manual' | 'moonraker' | 'octoprint' | 'prusalink'
export type PrinterState = 'idle' | 'printing' | 'paused' | 'done' | 'stopped' | 'error' | 'busy' | 'offline' | 'manual'

export interface Printer {
  id: number; name: string; model: string; kind: 'fdm' | 'resin'; connection: Connection; url: string; webcam_url: string
  bed_x: number; bed_y: number; bed_z: number; nozzle: number; materials: string; watts: number; wear_per_hour: number
  bought_on: string; price: number; start_hours: number; location: string; color: string; notes: string; spool_id: number | null
  archived: boolean; position: number; last_state: string; last_seen_at: string | null; home_ref: string; source: string; created_at: string
  hours: number; prints: number; failed: number; has_photo: boolean; has_key: boolean
}

export interface LiveStatus {
  state: PrinterState; message: string; file: string; job_id: string; progress: number; elapsed: number; left: number; filament_mm: number
  layer: number; layers: number; nozzle: number; nozzle_target: number; bed: number; bed_target: number; chamber?: number; software: string
}

export interface PrinterView extends Printer {
  state: PrinterState; live: LiveStatus | null; live_at: string; live_error: string; print: Print | null; progress: number; left: number; ends_at: string
  overdue: boolean; needs_clear: boolean; spool: Spool | null; next: Job | null; camera: boolean; upkeep_due: number
}

export interface Filament {
  id: number; brand: string; material: string; name: string; color: string; diameter: number; density: number; nozzle_temp: number; bed_temp: number
  net_weight: number; spool_weight: number; price: number; url: string; notes: string; external_id: string; source: string; created_at: string
  spools: number; left_grams: number
}

export interface Spool {
  id: number; filament_id: number; initial_g: number; used_g: number; price: number; bought_on: string; opened_on: string; dried_on: string
  location: string; lot: string; notes: string; archived: boolean; money_ref: string; inventory_ref: string; external_id: string; source: string
  last_used_at: string | null; created_at: string
  brand: string; material: string; name: string; color: string; density: number; diameter: number; spool_weight: number; filament_price: number
  left_g: number; printer_id: number | null; printer: string
}

export interface SpoolLog { id: number; spool_id: number; print_id: number | null; kind: 'print' | 'weigh' | 'fix'; grams: number; note: string; at: string; title: string }

export interface Model {
  id: number; name: string; description: string; tags: string; designer: string; license: string; url: string; favourite: boolean; archived: boolean
  source: string; created_at: string; has_cover: boolean; files: number; kinds: string; prints: number; good: number; last_print: string | null
  est_seconds: number; est_grams: number; size_x: number; size_y: number; size_z: number
}

export type FileKind = 'stl' | '3mf' | 'obj' | 'step' | 'gcode' | 'bgcode'

export interface ModelFile {
  id: number; model_id: number; name: string; kind: FileKind; size: number; sha256: string; size_x: number; size_y: number; size_z: number
  volume: number; triangles: number; est_seconds: number; est_grams: number; est_mm: number; layer_height: number; nozzle: number
  material: string; slicer: string; printer_model: string; settings: Record<string, string>; source: string; source_ref: string; created_at: string
  has_thumb: boolean; printable: boolean
}

export interface Job {
  id: number; title: string; model_id: number | null; file_id: number | null; printer_id: number | null; filament_id: number | null; material: string
  quantity: number; done_count: number; status: 'queued' | 'printing' | 'done' | 'cancelled'; position: number; due_on: string; for_whom: string
  contact_ref: string; price: number; notes: string; invoice_ref: string; task_ref: string; source: string; created_at: string; finished_at: string | null
  model_name: string; file_name: string; printer: string; filament: string; color: string; est_seconds: number; est_grams: number; has_cover: boolean
  printing: number; failed: number
}

export type PrintStatus = 'printing' | 'paused' | 'done' | 'failed' | 'cancelled'

export interface Print {
  id: number; printer_id: number | null; job_id: number | null; model_id: number | null; file_id: number | null; spool_id: number | null
  title: string; file_name: string; status: PrintStatus; started_at: string; ended_at: string | null; seconds: number; est_seconds: number
  progress: number; grams: number; est_grams: number; copies: number; fail_reason: string; fail_note: string; rating: number; notes: string
  settings: Record<string, string>; keep: boolean; filament_cost: number; power_cost: number; wear_cost: number; external_id: string
  inventory_ref: string; calendar_ref: string; source: string; created_at: string
  printer: string; spool: string; color: string; material: string; has_photo: boolean; has_cover: boolean; cost: number
}

export interface Upkeep {
  id: number; printer_id: number; title: string; every_hours: number; every_days: number; last_done_on: string; last_hours: number; notes: string
  task_ref: string; told_at: string; created_at: string; printer: string; due_hours: number | null; due_on: string; due: boolean; soon: boolean
}

export interface UpkeepLog { id: number; printer_id: number; upkeep_id: number | null; title: string; done_on: string; hours: number; cost: number; note: string; created_at: string; printer: string }

export interface Home { printers: number; printing: number; attention: number; queued: number; models: number; spools: number; low: number; upkeep_due: number; farm: PrinterView[] }

export interface Farm { printers: PrinterView[]; queue: Job[]; queued: number; counts: Record<string, number>; at: string; poll: number }
