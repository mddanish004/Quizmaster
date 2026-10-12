import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { setCsrf } from '../../lib/csrf'
import { useMe, type User } from '../auth/AuthContext'
import { Button, Field, Input } from '../../components/ui/primitives'

const schema = z.object({
  username: z.string().regex(/^[A-Za-z0-9_]{3,30}$/, '3-30 letters, numbers, underscores'),
  displayName: z.string().trim().min(1).max(80),
})

export function SettingsPage() {
  const me = useMe()
  const nav = useNavigate()
  const qc = useQueryClient()
  const [saved, setSaved] = useState(false)
  const [serverError, setServerError] = useState('')
  const { register, handleSubmit, formState } = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    values: me.data ? { username: me.data.data.user.username, displayName: me.data.data.user.displayName } : undefined,
  })

  if (me.isPending) return <main className="p-8">Loading settings…</main>
  if (me.isError || !me.data) return <main className="p-8"><p role="alert">Could not load settings.</p></main>
  const user: User = me.data.data.user

  const onSubmit = handleSubmit(async (v) => {
    setServerError('')
    setSaved(false)
    try {
      await api.patch('/api/v1/me', v)
      await qc.invalidateQueries({ queryKey: queryKeys.me })
      setSaved(true)
    } catch (e) {
      setServerError(e instanceof Error ? e.message : 'Could not save.')
    }
  })

  const logoutAll = async () => {
    await api.post('/api/v1/auth/logout-all')
    setCsrf('')
    await qc.invalidateQueries({ queryKey: queryKeys.me })
    nav('/login')
  }

  return (
    <main className="mx-auto max-w-[640px] px-6 py-8">
      <h1 className="text-2xl font-bold">Settings</h1>

      <section aria-label="Profile" className="mt-6 rounded-lg border border-line bg-surface p-4">
        <h2 className="font-semibold">Profile</h2>
        <p className="text-sm text-muted">{user.email} · verified: {user.emailVerified ? 'yes' : 'no'}</p>
        <form onSubmit={onSubmit} className="mt-3 flex flex-col gap-3" noValidate>
          <Field label="Username" error={formState.errors.username?.message}>
            <Input id="username" {...register('username')} autoComplete="username" />
          </Field>
          <Field label="Display name" error={formState.errors.displayName?.message}>
            <Input id="display-name" {...register('displayName')} autoComplete="nickname" />
          </Field>
          {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
          {saved && <p role="status" className="text-sm">Saved.</p>}
          <Button type="submit" disabled={formState.isSubmitting}>Save profile</Button>
        </form>
      </section>

      <section aria-label="Login methods" className="mt-4 rounded-lg border border-line bg-surface p-4">
        <h2 className="font-semibold">Login methods</h2>
        <ul className="mt-2 list-disc pl-5 text-sm">
          {user.loginMethods.map((m) => (
            <li key={m}>{m === 'password' ? 'Password' : 'Google'}</li>
          ))}
        </ul>
        <a href="/api/v1/auth/google/start?returnTo=/app/settings" className="mt-2 inline-block text-sm underline">
          Link a Google account
        </a>
      </section>

      <section aria-label="Sessions" className="mt-4 rounded-lg border border-line bg-surface p-4">
        <h2 className="font-semibold">Sessions</h2>
        <p className="text-sm text-muted">Sign out everywhere, including other browsers and devices.</p>
        <button onClick={logoutAll} className="mt-2 inline-flex min-h-[44px] items-center rounded-md border border-line px-4 text-sm">
          Sign out everywhere
        </button>
      </section>
    </main>
  )
}
