import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { Link } from 'react-router-dom'
import { api } from '../../lib/api'
import { Button, Field, Input } from '../../components/ui/primitives'

const schema = z.object({ email: z.string().email().max(254) })

export function ForgotPage() {
  const [sent, setSent] = useState(false)
  const { register, handleSubmit, formState } = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
  })
  const onSubmit = handleSubmit(async (v) => {
    await api.post('/api/v1/auth/password/forgot', v)
    setSent(true)
  })
  return (
    <main className="mx-auto w-full max-w-[440px] px-6 py-16">
      <h1 className="text-2xl font-bold">Reset your password</h1>
      {sent ? (
        <p role="status" className="mt-4 rounded-md border border-line bg-surface p-4">
          If an account exists for that email, a reset link is on its way. Check your inbox.
        </p>
      ) : (
        <form onSubmit={onSubmit} className="mt-6 flex flex-col gap-4" noValidate>
          <Field label="Email" error={formState.errors.email?.message}>
            <Input id="email" type="email" autoComplete="email" {...register('email')} />
          </Field>
          <Button type="submit" disabled={formState.isSubmitting}>
            {formState.isSubmitting ? 'Sending…' : 'Send reset link'}
          </Button>
        </form>
      )}
      <p className="mt-4 text-sm text-muted">
        <Link to="/login" className="underline">Back to sign in</Link>
      </p>
    </main>
  )
}
