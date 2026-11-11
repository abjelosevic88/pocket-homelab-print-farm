import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/farm.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import FarmPage from './pages/Farm'
import Printers, { PrinterPage } from './pages/Printers'
import Queue from './pages/Queue'
import Models, { ModelPage } from './pages/Models'
import FilamentPage from './pages/Filament'
import Prints, { PrintPage } from './pages/Prints'
import StatsPage from './pages/Stats'
import UpkeepPage from './pages/Upkeep'
import Settings from './pages/Settings'
import { DataProvider } from './lib/data'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><DataProvider><Settings /></DataProvider></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="farm"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<FarmPage />} />
        <Route path="/queue" element={<Queue />} />
        <Route path="/models" element={<Models />} />
        <Route path="/models/:id" element={<ModelPage />} />
        <Route path="/filament" element={<FilamentPage />} />
        <Route path="/prints" element={<Prints />} />
        <Route path="/prints/:id" element={<PrintPage />} />
        <Route path="/printers" element={<Printers />} />
        <Route path="/printers/:id" element={<PrinterPage />} />
        <Route path="/upkeep" element={<UpkeepPage />} />
        <Route path="/stats" element={<StatsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
