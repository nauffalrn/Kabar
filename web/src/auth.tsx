import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type User } from './api'

export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      try {
        return await api<User>('/auth/me')
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null
        throw e
      }
    },
    staleTime: 60_000,
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return async () => {
    await api('/auth/logout', { method: 'POST' })
    qc.setQueryData(['me'], null)
    qc.invalidateQueries()
  }
}

export const canEdit = (me: User | null | undefined, authorId: string) =>
  !!me && (me.role === 'admin' || me.author_id === authorId)
