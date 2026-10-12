import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { queryKeys } from '../../lib/query-client'
import { setCsrf } from '../../lib/csrf'

export type User = {
  id: string
  email: string
  username: string
  displayName: string
  emailVerified: boolean
  loginMethods: string[]
  createdAt: string
}

type MeResponse = { data: { user: User; csrfToken: string } }

export function useMe() {
  const q = useQuery({ queryKey: queryKeys.me, queryFn: () => api.get<MeResponse>('/api/v1/me'), retry: false })
  if (q.data) setCsrf(q.data.data.csrfToken)
  return q
}

export function useLogout() {
  const qc = useQueryClient()
  return async (all: boolean) => {
    await api.post(all ? '/api/v1/auth/logout-all' : '/api/v1/auth/logout')
    setCsrf('')
    await qc.invalidateQueries({ queryKey: queryKeys.me })
  }
}
