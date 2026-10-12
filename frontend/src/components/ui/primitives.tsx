import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react'

export function Button({
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { children: ReactNode }) {
  return (
    <button
      {...rest}
      className={`inline-flex min-h-[44px] items-center justify-center rounded-md bg-brand px-4 py-2 text-sm font-semibold text-white disabled:opacity-50 ${rest.className ?? ''}`}
    >
      {children}
    </button>
  )
}

export function Field({
  label,
  error,
  children,
  hint,
  htmlFor,
}: {
  label: string
  error?: string
  hint?: string
  htmlFor?: string
  children: ReactNode
}) {
  const id = htmlFor ?? label.toLowerCase().replace(/[^a-z0-9]+/g, '-')
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children}
      {hint && !error && <p className="text-xs text-muted">{hint}</p>}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={`min-h-[44px] rounded-md border border-line bg-surface px-3 text-base ${props.className ?? ''}`}
    />
  )
}

export function EmptyState({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-line bg-surface p-8 text-center">
      <p className="text-lg font-semibold">{title}</p>
      {action}
    </div>
  )
}

export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={`animate-pulse rounded bg-line ${className ?? 'h-10 w-full'}`} />
}
