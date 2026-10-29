import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { ApiError } from '../lib/api'
import { getStoredOwner, useAuth } from '../lib/auth'
import { McButton } from '../components/mc-button'
import { McInput } from '../components/mc-input'
import { McCard } from '../components/mc-card'

export const Route = createFileRoute('/login')({
  beforeLoad: () => {
    if (getStoredOwner()) throw redirect({ to: '/' })
  },
  component: LoginPage,
})

function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(email, password)
      await navigate({ to: '/' })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'could not reach the server')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-muted">
      <McCard className="w-full max-w-sm">
        <form onSubmit={onSubmit} className="flex w-full flex-col gap-4">
          <h1 className="header-xs text-foreground">m-Seismo dashboard</h1>

          <McInput
            id="email"
            label="Email"
            type="email"
            required
            autoFocus
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />

          <McInput
            id="password"
            label="Password"
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />

          {error && <p className="text-sm text-destructive">{error}</p>}

          <McButton type="submit" isLoading={submitting} className="w-full">
            Sign in
          </McButton>
        </form>
      </McCard>
    </div>
  )
}
