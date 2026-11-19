import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, query, type Category } from '../api'

export function useCategories() {
  return useQuery({ queryKey: ['categories'], queryFn: () => api<Category[]>('/categories'), staleTime: Infinity })
}

// Category links keep the current search, so filters combine.
export default function CategoryNav({ active, search, author }: { active: string; search: string; author: string }) {
  const { data: cats = [] } = useCategories()
  const link = (slug: string) => '/' + query({ category: slug, query: search, author })
  return (
    <nav aria-label="Kategori" className="catbar">
      <div className="wrap catbar__row">
        <Link to={link('')} aria-current={active === '' ? 'page' : undefined}>
          Semua
        </Link>
        {cats.map((c) => (
          <Link key={c.id} to={link(c.slug)} aria-current={active === c.slug ? 'page' : undefined}>
            {c.name}
          </Link>
        ))}
      </div>
    </nav>
  )
}
