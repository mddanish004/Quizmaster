import { StrictMode, Component, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route, Link } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from './lib/query-client'
import { ToastProvider } from './components/ui/toast'
import { LandingPage } from './features/landing/LandingPage'
import { RegisterPage } from './features/auth/RegisterPage'
import { LoginPage } from './features/auth/LoginPage'
import { ForgotPage } from './features/auth/ForgotPage'
import { ResetPage } from './features/auth/ResetPage'
import { RequireAuth, AppLayout } from './features/auth/AppLayout'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { QuizzesPage } from './features/quizzes/QuizzesPage'
import { EditorPage } from './features/quizzes/EditorPage'
import { PreviewPage } from './features/quizzes/PreviewPage'
import { GeneratorPage } from './features/ai-generation/GeneratorPage'
import { HostPage } from './features/live-host/HostPage'
import { JoinPage, ParticipantPage } from './features/participation/ParticipantPages'
import { HistoryPage, SessionDetailPage } from './features/results/HistoryPages'
import { SettingsPage } from './features/settings/SettingsPage'
import { useParams } from 'react-router-dom'
import './styles/index.css'

function SessionDetailRoute() {
  const { sessionId = '' } = useParams()
  return <SessionDetailPage sessionId={sessionId} />
}

class RootBoundary extends Component<{ children: ReactNode }, { failed: boolean; requestId: string }> {
  state = { failed: false, requestId: '' }
  static getDerivedStateFromError() {
    return { failed: true, requestId: Math.random().toString(36).slice(2) }
  }
  render() {
    if (this.state.failed) {
      return (
        <main className="mx-auto max-w-[440px] px-6 py-16">
          <h1 className="text-2xl font-bold">Something went wrong</h1>
          <p className="mt-2 text-muted">Request ID: <span className="mono">{this.state.requestId}</span></p>
          <Link to="/" className="mt-4 inline-block underline">Go home</Link>
        </main>
      )
    }
    return this.props.children
  }
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen">
      <a href="#main" className="sr-only focus:not-sr-only">Skip to content</a>
      <header className="border-b border-line bg-surface">
        <nav className="mx-auto flex max-w-[1280px] items-center justify-between px-6 py-3" aria-label="Primary">
          <Link to="/" className="font-bold">QuizForge AI</Link>
          <div className="flex gap-3 text-sm">
            <Link to="/join" className="inline-flex min-h-[44px] items-center">Join quiz</Link>
            <Link to="/login" className="inline-flex min-h-[44px] items-center">Sign in</Link>
          </div>
        </nav>
      </header>
      <div id="main">{children}</div>
    </div>
  )
}

function NotFound() {
  return (
    <main className="mx-auto max-w-[440px] px-6 py-16">
      <h1 className="text-2xl font-bold">Page not found</h1>
      <p className="mt-2 text-muted">Try the dashboard or joining a quiz.</p>
      <div className="mt-4 flex gap-3">
        <Link to="/app" className="underline">Dashboard</Link>
        <Link to="/join" className="underline">Join</Link>
      </div>
    </main>
  )
}

function App() {
  return (
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <BrowserRouter>
            <RootBoundary>
              <Routes>
                <Route path="/" element={<Shell><LandingPage /></Shell>} />
                <Route path="/register" element={<Shell><RegisterPage /></Shell>} />
                <Route path="/login" element={<Shell><LoginPage /></Shell>} />
                <Route path="/forgot-password" element={<Shell><ForgotPage /></Shell>} />
                <Route path="/reset-password" element={<Shell><ResetPage /></Shell>} />
                <Route
                  path="/app"
                  element={<RequireAuth><AppLayout><DashboardPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/quizzes"
                  element={<RequireAuth><AppLayout><QuizzesPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/quizzes/:id"
                  element={<RequireAuth><AppLayout><EditorPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/quizzes/:id/preview"
                  element={<RequireAuth><AppLayout><PreviewPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/generate"
                  element={<RequireAuth><AppLayout><GeneratorPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/host/:sessionId"
                  element={<RequireAuth><HostPage /></RequireAuth>}
                />
                <Route
                  path="/app/sessions"
                  element={<RequireAuth><AppLayout><HistoryPage /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/sessions/:sessionId"
                  element={<RequireAuth><AppLayout><SessionDetailRoute /></AppLayout></RequireAuth>}
                />
                <Route
                  path="/app/settings"
                  element={<RequireAuth><AppLayout><SettingsPage /></AppLayout></RequireAuth>}
                />
                <Route path="/join" element={<Shell><JoinPage /></Shell>} />
                <Route path="/play/:sessionId" element={<ParticipantPage />} />
                <Route path="*" element={<Shell><NotFound /></Shell>} />
              </Routes>
            </RootBoundary>
          </BrowserRouter>
        </ToastProvider>
      </QueryClientProvider>
    </StrictMode>
  )
}

createRoot(document.getElementById('root')!).render(<App />)
