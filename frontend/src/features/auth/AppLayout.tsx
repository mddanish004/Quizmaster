import type { ReactNode } from 'react'
import { Navigate, Link, useNavigate } from 'react-router-dom'
import { useMe, useLogout } from './AuthContext'
import { Skeleton } from '../../components/ui/primitives'

export function RequireAuth({ children }: { children: ReactNode }) {
  const me = useMe()
  if (me.isPending) {
    return (
      <main className="mx-auto max-w-[1280px] px-6 py-8" aria-busy="true">
        <Skeleton className="h-10 w-48" />
        <Skeleton className="mt-4 h-32" />
      </main>
    )
  }
  if (me.isError) return <Navigate to="/login" replace />
  return <>{children}</>
}

export function AppLayout({ children }: { children: ReactNode }) {
  const me = useMe()
  const logout = useLogout()
  const nav = useNavigate()
  const onLogout = async () => {
    await logout(false)
    nav('/login')
  }
  return (
    <div className="min-h-screen lg:flex">
      <nav aria-label="Creator" className="border-b border-line bg-surface lg:w-56 lg:border-b-0 lg:border-r">
        <div className="flex items-center justify-between px-4 py-3 lg:flex-col lg:items-stretch lg:gap-2">
          <Link to="/app" className="font-bold">QuizForge AI</Link>
          <div className="flex gap-4 text-sm lg:flex-col">
            <Link to="/app" className="inline-flex min-h-[44px] items-center">Dashboard</Link>
            <Link to="/app/quizzes" className="inline-flex min-h-[44px] items-center">Quizzes</Link>
            <Link to="/app/sessions" className="inline-flex min-h-[44px] items-center">Sessions</Link>
            <Link to="/app/settings" className="inline-flex min-h-[44px] items-center">Settings</Link>
          </div>
          <div className="hidden items-center gap-2 lg:flex">
            <span className="text-sm text-muted">{me.data?.data.user.displayName}</span>
            <button onClick={onLogout} className="inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm">
              Sign out
            </button>
          </div>
        </div>
        <div className="flex items-center justify-between border-t border-line px-4 py-2 lg:hidden">
          <span className="text-sm text-muted">{me.data?.data.user.displayName}</span>
          <button onClick={onLogout} className="inline-flex min-h-[44px] items-center rounded-md border border-line px-3 text-sm">
            Sign out
          </button>
        </div>
      </nav>
      <div className="flex-1">{children}</div>
    </div>
  )
}
