import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate, useSearchParams, Link } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'

const schema = z.object({ password: z.string().min(12).max(128) })

export function ResetPage() {
  const [params] = useSearchParams()
  const nav = useNavigate()
  const [serverError, setServerError] = useState('')
  const token = params.get('token') ?? ''
  const { register, handleSubmit, formState } = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
  })
  const onSubmit = handleSubmit(async (v) => {
    setServerError('')
    try {
      await api.post('/api/v1/auth/password/reset', { token, password: v.password })
      nav('/login')
    } catch (e) {
      setServerError(e instanceof Error ? e.message : 'Reset failed. The link may have expired.')
    }
  })
  if (!token) {
    return (
      <main className="mx-auto w-full max-w-[440px] px-6 py-16">
        <p role="alert">That reset link is incomplete. Request a new one.</p>
        <Link to="/forgot-password" className="underline">Request reset link</Link>
      </main>
    )
  }
  return (
    <main className="mx-auto w-full max-w-[440px] px-6 py-16">
      <h1 className="text-2xl font-bold">Choose a new password</h1>
      <form onSubmit={onSubmit} className="mt-6 flex flex-col gap-4" noValidate>
        <Field label="Password" error={formState.errors.password?.message} hint="12-128 characters.">
          <Input id="password" type="password" autoComplete="new-password" {...register('password')} />
        </Field>
        {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
        <Button type="submit" disabled={formState.isSubmitting}>
          {formState.isSubmitting ? 'Saving…' : 'Set new password'}
        </Button>
      </form>
    </main>
  )
}
