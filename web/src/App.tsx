import { Route, Routes } from 'react-router-dom'
import Header from './components/Header'
import FeedPage from './pages/FeedPage'
import ArticlePage from './pages/ArticlePage'
import ProfilePage from './pages/ProfilePage'
import AuthPage from './pages/AuthPage'
import WritePage from './pages/WritePage'
import AdminPage from './pages/AdminPage'
import NotFound from './pages/NotFound'

export default function App() {
  return (
    <>
      <Header />
      <Routes>
        <Route path="/" element={<FeedPage />} />
        <Route path="/artikel/:id" element={<ArticlePage />} />
        <Route path="/artikel/:id/edit" element={<WritePage />} />
        <Route path="/penulis/:id" element={<ProfilePage />} />
        <Route path="/tulis" element={<WritePage />} />
        <Route path="/masuk" element={<AuthPage mode="login" />} />
        <Route path="/daftar" element={<AuthPage mode="register" />} />
        <Route path="/moderasi" element={<AdminPage />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
      <footer className="footer">
        <div className="wrap">Kabar · artikel dari siapa saja</div>
      </footer>
    </>
  )
}
