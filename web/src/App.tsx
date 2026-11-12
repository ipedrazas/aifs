import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router'
import { makeQueryClient } from './api/queryClient'
import { AuthGate } from './components/AuthGate'
import { Layout } from './components/Layout'
import { Home } from './pages/Home'
import { NotFound } from './pages/NotFound'

export default function App() {
  const [queryClient] = useState(makeQueryClient)
  return (
    <QueryClientProvider client={queryClient}>
      <AuthGate>
        <BrowserRouter>
          <Routes>
            <Route element={<Layout />}>
              <Route index element={<Home />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthGate>
    </QueryClientProvider>
  )
}
