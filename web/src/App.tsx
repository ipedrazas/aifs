import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { makeQueryClient } from './api/queryClient'
import { AuthGate } from './components/AuthGate'
import { Layout } from './components/Layout'
import { NotFound } from './pages/NotFound'
import { NewProduct } from './pages/products/NewProduct'
import { ProductDetail } from './pages/products/ProductDetail'
import { ProductList } from './pages/products/ProductList'

export default function App() {
  const [queryClient] = useState(makeQueryClient)
  return (
    <QueryClientProvider client={queryClient}>
      <AuthGate>
        <BrowserRouter>
          <Routes>
            <Route element={<Layout />}>
              <Route index element={<Navigate to="/products" replace />} />
              <Route path="products" element={<ProductList />} />
              <Route path="products/new" element={<NewProduct />} />
              <Route path="products/:id" element={<ProductDetail />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthGate>
    </QueryClientProvider>
  )
}
