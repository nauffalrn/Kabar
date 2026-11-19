import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, query, type Article, type Page } from './api'

// Keyset pagination has no page numbers: we remember the cursors we
// followed, so "newer" steps back and "older" follows next_cursor.
export function usePagedArticles(filters: Record<string, string>) {
  const filterKey = JSON.stringify(filters)
  const [state, setState] = useState<{ key: string; stack: string[] }>({ key: filterKey, stack: [] })
  const stack = state.key === filterKey ? state.stack : []
  const cursor = stack[stack.length - 1]

  const result = useQuery({
    queryKey: ['articles', filters, cursor ?? ''],
    queryFn: () => api<Page<Article>>('/articles' + query({ ...filters, cursor })),
    placeholderData: keepPreviousData,
  })

  const scrollTop = () => window.scrollTo({ top: 0, behavior: 'smooth' })
  return {
    ...result,
    isFirstPage: stack.length === 0,
    hasOlder: !!result.data?.next_cursor,
    older: () => {
      if (!result.data?.next_cursor) return
      setState({ key: filterKey, stack: [...stack, result.data.next_cursor] })
      scrollTop()
    },
    newer: () => {
      setState({ key: filterKey, stack: stack.slice(0, -1) })
      scrollTop()
    },
  }
}
