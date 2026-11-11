// Left column: start a print, the sections, and each printer as it is now.
import { useCallback, useEffect, useState } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import { STATE } from '../lib/farm'
import type { Home } from '../lib/types'
import { Icon } from './icons'

export function Sidebar({ onCreate, onNavigate }: { onCreate: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [home, setHome] = useState<Home | null>(null)
  const load = useCallback(() => { api.get<Home>(`${V1}/home`).then(setHome).catch(() => {}) }, [])
  useEffect(() => { load() }, [load, loc.pathname])
  useOnChange(load)
  useEffect(() => {
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') load() }, 15000)
    return () => window.clearInterval(t)
  }, [load])

  const title = settings?.title || status?.title || 'Pocket Print Farm'
  const path = loc.pathname
  const nav: { to: string; label: string; icon: string; active: boolean; count?: number | string; strong?: boolean; hint?: string }[] = [
    { to: '/', label: 'Farm', icon: 'grid', active: path === '/', count: home?.printing ? `${home.printing} printing` : undefined, strong: !!home?.attention, hint: home?.attention ? 'A printer needs you' : undefined },
    { to: '/queue', label: 'Queue', icon: 'list', active: path.startsWith('/queue'), count: home?.queued || undefined },
    { to: '/models', label: 'Models', icon: 'package', active: path.startsWith('/models'), count: home?.models || undefined },
    { to: '/filament', label: 'Filament', icon: 'disc', active: path.startsWith('/filament'), count: home?.low ? `${home.low} low` : undefined, strong: !!home?.low },
    { to: '/prints', label: 'History', icon: 'history', active: path.startsWith('/prints') },
    { to: '/printers', label: 'Printers', icon: 'printer', active: path.startsWith('/printers') },
    { to: '/upkeep', label: 'Upkeep', icon: 'wrench', active: path.startsWith('/upkeep'), count: home?.upkeep_due ? `${home.upkeep_due} due` : undefined, strong: !!home?.upkeep_due },
    { to: '/stats', label: 'Numbers', icon: 'chart', active: path.startsWith('/stats') },
  ]
  return (
    <aside className="sidebar" aria-label="Pocket Print Farm">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Print Farm' ? title : <>Pocket <b>Print Farm</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onCreate} title="Start a print (N)">
        <Icon name="play" size={15} strokeWidth={2.2} />Start a print
      </button>

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(x => (
          <NavLink key={x.to} to={x.to} onClick={onNavigate} title={x.hint} className={() => `sb-nav-item ${x.active ? 'active' : ''}`}>
            <Icon name={x.icon} size={16} />{x.label}
            {!!x.count && <span className={`sb-count ${x.strong ? 'strong' : ''}`}>{x.count}</span>}
          </NavLink>
        ))}
      </nav>

      {!!home?.farm.length && <section className="sb-sec">
        <h4 className="sb-h">Printers</h4>
        {home.farm.map(p => {
          const s = STATE[p.state] || STATE.idle
          return (
            <button key={p.id} type="button" className={`sb-row sb-list ${path === `/printers/${p.id}` ? 'active' : ''}`} onClick={() => { onNavigate(); navigate(`/printers/${p.id}`) }} title={`${p.name}: ${s.label}${p.print ? ' — ' + p.print.title : ''}`}>
              <span className={`sb-state tone-${s.tone}`} />
              <span className="grow ellipsis">{p.name}</span>
              {p.print ? <span className="muted small nowrap tnum">{Math.round(p.progress * 100)}%</span> : p.needs_clear ? <span className="muted small nowrap">clear</span> : null}
            </button>
          )
        })}
      </section>}

      <div className="sb-foot">
        <NavLink to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</NavLink>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('ppf:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
