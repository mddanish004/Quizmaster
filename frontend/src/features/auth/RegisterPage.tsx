import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate, Link } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'

const schema = z.object({
  email: z.string().email().max(254),
  username: z.string().regex(/^[A-Za-z0-9_]{3,30}$/, '3-30 letters, numbers, underscores'),
  displayName: z.string().trim().min(1).max(80),
  password: z.string().min(12).max(128),
})
type Form = z.infer<typeof schema>

export function RegisterPage() {
  const nav = useNavigate()
  const [serverError, setServerError] = useState('')
  const { register, handleSubmit, formState } = useForm<Form>({ resolver: zodResolver(schema) })
  const onSubmit = handleSubmit(async (v) => {
    setServerError('')
    try {
      await api.post('/api/v1/auth/register', v)
      nav('/app')
    } catch (e) {
      setServerError(e instanceof Error ? e.message : 'Registration failed')
    }
  })
  return (
    <main className="mx-auto w-full max-w-[440px] px-6 py-16">
      <h1 className="text-2xl font-bold">Create your account</h1>
      <form onSubmit={onSubmit} className="mt-6 flex flex-col gap-4" noValidate>
        <Field label="Email" error={formState.errors.email?.message}>
          <Input id="email" type="email" autoComplete="email" {...register('email')} />
        </Field>
        <Field label="Username" error={formState.errors.username?.message}>
          <Input id="username" autoComplete="username" {...register('username')} />
        </Field>
        <Field label="Display name" error={formState.errors.displayName?.message}>
          <Input id="display-name" autoComplete="nickname" {...register('displayName')} />
        </Field>
        <Field label="Password" error={formState.errors.password?.message} hint="12-128 characters. Use a passphrase.">
          <Input id="password" type="password" autoComplete="new-password" {...register('password')} />
        </Field>
        {serverError && <p role="alert" className="text-sm text-danger">{serverError}</p>}
        <Button type="submit" disabled={formState.isSubmitting}>
          {formState.isSubmitting ? 'Creating…' : 'Create account'}
        </Button>
      </form>
      <p className="mt-4 text-sm text-muted">
        Already have an account? <Link to="/login" className="underline">Sign in</Link>
      </p>
    </main>
  )
}
