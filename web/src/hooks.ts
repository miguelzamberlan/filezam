import { useEffect, useMemo } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { Api, ApiError, setUnauthorizedHandler } from './api/client'
import type { Entry, Listing } from './api/types'
import type { Sort } from './lib/naturalSort'
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

// Tamanho de página da listagem: pastas com até isso chegam numa requisição; acima, as
// páginas seguintes são pedidas conforme a rolagem se aproxima do fim.
export const LIST_PAGE = 2000

export interface ListingData {
  entries: Entry[]
  total: number
  hidden: number
}

// useListing pagina no servidor (ordenação e filtro de ocultos também lá), devolvendo as
// páginas já concatenadas. A chave inclui ordenação/ocultos: mudá-los refaz a consulta.
export function useListing(path: string, sort: Sort, showHidden: boolean, enabled = true) {
  const q = useInfiniteQuery<Listing, ApiError, { pages: Listing[] }, readonly unknown[], number>({
    queryKey: [...listKey(path), sort.key, sort.dir, showHidden],
    queryFn: ({ pageParam }) => Api.list(path, { offset: pageParam, limit: LIST_PAGE, sort: sort.key, dir: sort.dir, hidden: showHidden }),
    initialPageParam: 0,
    getNextPageParam: (last) => {
      const next = (last.offset ?? 0) + last.entries.length
      return next < (last.total ?? 0) ? next : undefined
    },
    enabled,
  })
  const data = useMemo<ListingData | undefined>(() => {
    if (!q.data) return undefined
    const first = q.data.pages[0]
    return { entries: q.data.pages.flatMap((p) => p.entries), total: first?.total ?? 0, hidden: first?.hidden ?? 0 }
  }, [q.data])
  return { data, isLoading: q.isLoading, isFetching: q.isFetching, error: q.error, hasNextPage: q.hasNextPage, isFetchingNextPage: q.isFetchingNextPage, fetchNextPage: q.fetchNextPage }
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
