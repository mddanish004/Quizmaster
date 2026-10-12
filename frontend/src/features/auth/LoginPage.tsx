import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate, Link, useSearchParams } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'

const schema = z.object({
  identifier: z.string().min(1).max(254),
  password: z.string().min(1).max(128),
})
type Form = z.infer<typeof schema>

export function LoginPage() {
  const nav = useNavigate()
  const [params] = useSearchParams()
  const [serverError, setServerError] = useState('')
  const [show, setShow] = useState(false)
  const oauth = params.get('oauth')
  const link = params.get('link')
  const { register, handleSubmit, formState } = useForm<Form>({ resolver: zodResolver(schema) })
  const onSubmit = handleSubmit(async (v) => {
    setServerError('')
    try {
      await api.post('/api/v1/auth/login', v)
      nav('/app')
    } catch {
      setServerError('Invalid credentials. Check your email/username and password.')
    }
  })
  return (
    <main className="mx-auto w-full max-w-[440px] px-6 py-16">
      <h1 className="text-2xl font-bold">Sign in</h1>
      {oauth === 'failed' && (
        <p role="alert" className="mt-4 rounded-md border border-danger p-3 text-sm">
          Google sign-in did not complete. Try again.
        </p>
      )}
      {link === 'required' && (
        <p role="alert" className="mt-4 rounded-md border border-line bg-surface p-3 text-sm">
          An account with that email already uses a password. Sign in below, then link Google from
          settings.
        </p>
      )}
      <a href="/api/v1/auth/google/start" className="mt-4 inline-flex min-h-[44px] w-full items-center justify-center rounded-md border border-line bg-surface font-semibold">
        Continue with Google
      </a>
      <div className="my-4 border-t border-line" role="separator" />
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <Field label="Identifier" error={formState.errors.identifier?.message}>
          <Input id="identifier" autoComplete="username" {...register('identifier')} />
        </Field>
        <Field label="Password" error={formState.errors.password?.message}>
          <div className="flex gap-2">
            <Input id="password" type={show ? 'text' : 'password'} autoComplete="current-password" {...register('password')} className="flex-1" />
            <button type="button" onClick={() => setShow((s) => !s)} aria-label={show ? 'Hide password' : 'Show password'} className="min-h-[44px] min-w-[44px] rounded-md border border-line">
              {show ? 'Hide' : 'Show'}
            </button>
          </div>
        </Field>
        {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
        <Button type="submit" disabled={formState.isSubmitting}>
          {formState.isSubmitting ? 'Signing in…' : 'Sign in'}
        </Button>
      </form>
      <p className="mt-4 text-sm text-muted">
        No account? <Link to="/register" className="underline">Create one</Link>
        {' · '}
        <Link to="/forgot-password" className="underline">Forgot password</Link>
      </p>
    </main>
  )
}
