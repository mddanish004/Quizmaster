import { createContext, useContext, useState, type ReactNode } from 'react'

type Toast = { id: number; message: string }
const ToastContext = createContext<(message: string) => void>(() => {})

let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const push = (message: string) => {
    const id = nextId++
    setToasts((t) => [...t, { id, message }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000)
  }
  return (
    <ToastContext.Provider value={push}>
      {children}
      <div aria-live="polite" className="fixed bottom-4 right-4 flex flex-col gap-2">
        {toasts.map((t) => (
          <div key={t.id} role="status" className="rounded-md bg-ink px-4 py-2 text-sm text-white shadow">
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export const useToast = () => useContext(ToastContext)
