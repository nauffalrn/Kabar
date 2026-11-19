import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { ApiError } from './api'
import App from './App'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      // Retry only transient failures; a 4xx will not fix itself.
      retry: (count, err) => count < 2 && (!(err instanceof ApiError) || err.status >= 500),
      retryDelay: (count, err) => (err instanceof ApiError && err.retryAfter ? err.retryAfter * 1000 : 500 * 2 ** count),
    },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
