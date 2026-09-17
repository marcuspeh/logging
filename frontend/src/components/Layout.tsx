import type { ReactNode } from "react";
import { Link } from "react-router-dom";

interface Props {
  children: ReactNode;
}

// Top-level chrome: brand + nav. Health indicator deliberately omitted
// (per project decision — no polling, no extra component).
export function Layout({ children }: Props) {
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-12 max-w-screen-2xl items-center gap-6 px-4">
          <Link to="/" className="font-semibold tracking-tight">
            Logs
          </Link>
          <nav className="flex items-center gap-4 text-sm">
            <Link
              to="/"
              className="text-slate-600 hover:text-slate-900"
            >
              Search
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-screen-2xl px-4 py-6">{children}</main>
    </div>
  );
}
