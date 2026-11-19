import { Link } from 'react-router-dom'

export default function NotFound() {
  return (
    <main className="page-narrow">
      <h1 className="page-title">Halaman tidak ditemukan</h1>
      <p style={{ fontSize: 18, lineHeight: 1.6 }}>Mungkin artikelnya sudah dihapus, atau tautannya salah ketik.</p>
      <Link to="/" className="btn btn--blue">
        Kembali ke beranda
      </Link>
    </main>
  )
}
