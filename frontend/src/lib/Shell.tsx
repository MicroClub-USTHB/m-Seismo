import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useAuth } from './auth'

export function Shell({ children }: { children: ReactNode }) {
  const { owner, logout } = useAuth()

  return (
    <div className="min-h-screen bg-gray-50">
      <header className="flex items-center justify-between border-b bg-white px-6 py-4">
        <div className="flex items-center gap-6">
          <h1 className="text-lg font-semibold text-gray-900">m-Seismo</h1>
          <nav className="flex items-center gap-1">
            <Link
              to="/"
              className="rounded px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-100"
              activeProps={{ className: 'rounded px-3 py-1.5 text-sm font-medium bg-gray-900 text-white' }}
            >
              Users
            </Link>
            <Link
              to="/apps"
              className="rounded px-3 py-1.5 text-sm font-medium text-gray-600 hover:bg-gray-100"
              activeProps={{ className: 'rounded px-3 py-1.5 text-sm font-medium bg-gray-900 text-white' }}
            >
              Apps
            </Link>
          </nav>
        </div>
        <div className="flex items-center gap-4 text-sm text-gray-600">
          <span>{owner?.email}</span>
          <button onClick={logout} className="text-gray-900 underline hover:no-underline">
            Sign out
          </button>
        </div>
      </header>

      <main className="mx-auto max-w-4xl space-y-8 p-6">{children}</main>
    </div>
  )
}
