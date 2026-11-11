// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { DataProvider } from '../lib/data'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'
import { StartDialog } from './dialogs'

export { announceChange, useOnChange } from '../lib/data'

const SIDEBAR_KEY = 'ppf-sidebar'

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string, to?: string) => void>(() => {})
export function useCrumb(title: string | undefined, parent?: string) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || '', parent); return () => set('') }, [title, parent, set])
}

/** Start a print from anywhere (a page's button). */
export const openCreate = (template?: string) => window.dispatchEvent(new CustomEvent('ppf:create', { detail: { template } }))

export default function Layout() {
  return <DataProvider><Shell /></DataProvider>
}

function Shell() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState<{ t: string; to?: string }>({ t: '' })
  const setCrumb = useCallback((t: string, to?: string) => setCrumbRaw({ t, to }), [])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)

  const [starting, setStarting] = useState(false)
  const create = () => {
    closeDrawer()
    setStarting(true)
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = () => createRef.current()
    window.addEventListener('ppf:shortcuts', open)
    window.addEventListener('ppf:create', make)
    return () => { window.removeEventListener('ppf:shortcuts', open); window.removeEventListener('ppf:create', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': e.preventDefault(); createRef.current(); break
        case 'h': go('/'); break
        case 'q': go('/queue'); break
        case 'm': go('/models'); break
        case 'f': go('/filament'); break
        case 'y': go('/prints'); break
        case 'p': go('/printers'); break
        case 'u': go('/upkeep'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'start', label: 'Start a print', icon: 'play', hint: 'N', keywords: 'print printer job begin', run: () => create() },
    { id: 'job', label: 'Add to the queue', icon: 'list-plus', keywords: 'job queue todo', run: () => navigate('/queue?new=1') },
    { id: 'models-add', label: 'Add models', icon: 'upload', keywords: 'stl 3mf gcode obj step upload files link', run: () => navigate('/models?add=1') },
    { id: 'farm', label: 'The farm', icon: 'grid', hint: 'H', keywords: 'home printers now', run: () => navigate('/') },
    { id: 'queue', label: 'The queue', icon: 'list', hint: 'Q', run: () => navigate('/queue') },
    { id: 'models', label: 'Models', icon: 'package', hint: 'M', keywords: 'library stl', run: () => navigate('/models') },
    { id: 'fav', label: 'Favourite models', icon: 'star', run: () => navigate('/models?filter=fav') },
    { id: 'filament', label: 'Filament', icon: 'disc', hint: 'F', keywords: 'spools pla petg weigh', run: () => navigate('/filament') },
    { id: 'spoolman', label: 'Bring spools over from Spoolman', icon: 'download', keywords: 'import', run: () => navigate('/filament') },
    { id: 'history', label: 'History', icon: 'history', hint: 'Y', keywords: 'prints failed done', run: () => navigate('/prints') },
    { id: 'printers', label: 'Printers', icon: 'printer', hint: 'P', keywords: 'klipper octoprint prusalink', run: () => navigate('/printers') },
    { id: 'upkeep', label: 'Upkeep', icon: 'wrench', hint: 'U', keywords: 'maintenance nozzle belts', run: () => navigate('/upkeep') },
    { id: 'stats', label: 'Numbers', icon: 'chart', keywords: 'stats hours costs', run: () => navigate('/stats') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'link', keywords: 'files inventory money invoicing contacts tasks calendar chat parcels projects home alerts core', run: () => navigate('/settings/apps') },
    { id: 'notif', label: 'Settings: notifications', icon: 'bell', keywords: 'push reminder', run: () => navigate('/settings/notifications') },
    { id: 'ai-set', label: 'Settings: AI', icon: 'sparkles', keywords: 'pocket ai openai', run: () => navigate('/settings/ai') },
    { id: 'export', label: 'Settings: export and import', icon: 'download', keywords: 'backup json csv', run: () => navigate('/settings/data') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onCreate={create} onNavigate={closeDrawer} />
        </div>
        <div className="main">
          <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
          <main className="content"><div className="content-inner"><Outlet /></div></main>
        </div>
        {mobile && loc.pathname === '/' && <button type="button" className="fab" aria-label="Start a print" title="Start a print" onClick={() => create()}><Icon name="play" size={24} strokeWidth={2} /></button>}
        {mobile && loc.pathname === '/models' && <button type="button" className="fab" aria-label="Add models" title="Add models" onClick={() => navigate('/models?add=1')}><Icon name="plus" size={26} strokeWidth={2} /></button>}
        {mobile && loc.pathname === '/queue' && <button type="button" className="fab" aria-label="Add to the queue" title="Add to the queue" onClick={() => navigate('/queue?new=1')}><Icon name="plus" size={26} strokeWidth={2} /></button>}
        {starting && <StartDialog onClose={() => setStarting(false)} onStarted={() => navigate('/')} />}
        {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      </div>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = {
  '': ['Farm', '/'], queue: ['Queue', '/queue'], models: ['Models', '/models'], filament: ['Filament', '/filament'], prints: ['History', '/prints'],
  printers: ['Printers', '/printers'], upkeep: ['Upkeep', '/upkeep'], stats: ['Numbers', '/stats'],
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: { t: string; to?: string } }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('ppf:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb.t ? <><button type="button" className="crumb" onClick={() => navigate(crumb.to || sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb.t}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
