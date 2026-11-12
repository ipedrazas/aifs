import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { makeQueryClient } from './api/queryClient'
import { AuthGate } from './components/AuthGate'
import { Layout } from './components/Layout'
import { ImageDetailPage } from './pages/images/ImageDetailPage'
import { ModelDetail } from './pages/models/ModelDetail'
import { ModelList } from './pages/models/ModelList'
import { NewModel } from './pages/models/NewModel'
import { NotFound } from './pages/NotFound'
import { NewPhotoshoot } from './pages/photoshoots/NewPhotoshoot'
import { PhotoshootDetail } from './pages/photoshoots/PhotoshootDetail'
import { PhotoshootList } from './pages/photoshoots/PhotoshootList'
import { NewProduct } from './pages/products/NewProduct'
import { ReviewGallery } from './pages/review/ReviewGallery'
import { ReviewScreen } from './pages/review/ReviewScreen'
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
              <Route path="models" element={<ModelList />} />
              <Route path="models/new" element={<NewModel />} />
              <Route path="models/:id" element={<ModelDetail />} />
              <Route path="photoshoots" element={<PhotoshootList />} />
              <Route path="photoshoots/new" element={<NewPhotoshoot />} />
              <Route path="photoshoots/:id" element={<PhotoshootDetail />} />
              <Route path="images/:id" element={<ImageDetailPage />} />
              <Route path="review" element={<ReviewGallery />} />
              <Route path="review/:id" element={<ReviewScreen />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthGate>
    </QueryClientProvider>
  )
}
