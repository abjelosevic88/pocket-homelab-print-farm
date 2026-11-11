// The other Pocket apps Print Farm works with.
import type { AppSlug, AppStateName } from './types'

export const APP_INFO: Record<AppSlug, { name: string; icon: string }> = {
  files: { name: 'Pocket Files', icon: 'archive' }, inventory: { name: 'Pocket Inventory', icon: 'package' }, money: { name: 'Pocket Money', icon: 'wallet' },
  invoicing: { name: 'Pocket Invoicing', icon: 'receipt' }, contacts: { name: 'Pocket Contacts', icon: 'users' }, tasks: { name: 'Pocket Tasks', icon: 'check-square' },
  calendar: { name: 'Pocket Calendar', icon: 'calendar' }, chat: { name: 'Pocket Chat', icon: 'message' }, parcels: { name: 'Pocket Parcels', icon: 'package' },
  projects: { name: 'Pocket Projects', icon: 'layers' }, home: { name: 'Pocket Home', icon: 'home' }, alerts: { name: 'Pocket Alerts', icon: 'bell' },
}

export const STATE_WORDS: Record<AppStateName, string> = {
  on: 'Connected', partly: 'Partly connected', off: 'Off', link_off: 'Link is off in Pocket Core', missing: 'Not on this box', old: 'Needs an update',
  no_core: 'No Pocket Core', not_set_up: 'Not set up', error: 'Not working',
}
