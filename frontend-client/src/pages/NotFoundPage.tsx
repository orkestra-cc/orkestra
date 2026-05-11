import { Link } from 'react-router-dom';

export function NotFoundPage() {
  return (
    <section className="mx-auto max-w-xl px-6 py-24 text-center">
      <p className="mb-3 text-7xl font-semibold tracking-tight text-slate-300">
        404
      </p>
      <h1 className="mb-6 text-2xl font-semibold text-slate-900">
        Pagina non trovata
      </h1>
      <Link to="/" className="text-slate-600 underline hover:text-slate-900">
        Torna alla home
      </Link>
    </section>
  );
}
