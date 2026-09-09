import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { Api, ApiError, setUnauthorizedHandler } from './api/client'
import type { Listing } from './api/types'
import { uploadManager } from './upload/manager'

export function useAuth() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const q = useQuery({
    queryKey: ['me'],
    queryFn: () => Api.me(),
    retry: false,
    staleTime: 60_000,
  })
  useEffect(() => {
    setUnauthorizedHandler(() => {
      qc.setQueryData(['me'], null)
      qc.removeQueries({ predicate: (x) => x.queryKey[0] !== 'me' })
      navigate('/login', { replace: true })
    })
  }, [qc, navigate])
  return {
    user: q.data?.user ?? null,
    loading: q.isLoading,
    error: q.error instanceof ApiError ? q.error : null,
    refresh: () => qc.invalidateQueries({ queryKey: ['me'] }),
  }
}

export function useConfig() {
  const q = useQuery({ queryKey: ['config'], queryFn: () => Api.config(), staleTime: Infinity })
  useEffect(() => {
    if (q.data) {
      uploadManager.configure(q.data)
      void uploadManager.loadPending()
    }
  }, [q.data])
  return q.data ?? null
}

// useConfigValue lê a configuração já em cache, sem os efeitos colaterais de useConfig.
export function useConfigValue() {
  return useQuery({ queryKey: ['config'], queryFn: () => Api.config(), staleTime: Infinity }).data ?? null
}

export const listKey = (path: string) => ['list', path] as const

export function useListing(path: string, enabled = true) {
  return useQuery<Listing, ApiError>({ queryKey: listKey(path), queryFn: () => Api.list(path), enabled })
}

export function useInvalidateDirs() {
  const qc = useQueryClient()
  return (dirs: string[]) => {
    for (const d of dirs) void qc.invalidateQueries({ queryKey: listKey(d) })
    void qc.invalidateQueries({ queryKey: ['disk'] })
    void qc.invalidateQueries({ queryKey: ['info'] })
  }
}

export function useFavorites() {
  return useQuery({ queryKey: ['favorites'], queryFn: () => Api.favorites(), staleTime: 60_000 })
}
