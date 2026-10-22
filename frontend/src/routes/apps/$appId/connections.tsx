import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  api,
  ApiError,
  ALL_TRACKERS,
  type App,
  type AppProjectLink,
  type AppKey,
  type AppKeyCreated,
  type Connection,
  type CredentialStatus,
  type Project,
  type Target,
  type Tracker,
} from '../../../lib/api'
import { getStoredOwner } from '../../../lib/auth'
import { Shell } from '../../../lib/Shell'

interface CallbackSearch {
  provider?: string
  status?: string
  reason?: string
  connection_id?: string
}

export const Route = createFileRoute('/apps/$appId/connections')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  validateSearch: (search: Record<string, unknown>): CallbackSearch => ({
    provider: typeof search.provider === 'string' ? search.provider : undefined,
    status: typeof search.status === 'string' ? search.status : undefined,
    reason: typeof search.reason === 'string' ? search.reason : undefined,
    connection_id: typeof search.connection_id === 'string' ? search.connection_id : undefined,
  }),
  component: AppConnectionsPage,
})

function AppConnectionsPage() {
  const { appId } = Route.useParams()
  const search = Route.useSearch()

  const app = useQuery({
    queryKey: ['apps', appId],
    queryFn: () => api.get<App>(`/apps/${appId}`),
  })

  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.get<{ providers: Tracker[] }>('/providers'),
  })

  const connections = useQuery({
    queryKey: ['apps', appId, 'connections'],
    queryFn: () => api.get<Connection[]>(`/apps/${appId}/connections`),
  })

  return (
    <Shell>
      <Link to="/apps" className="text-sm text-gray-600 underline hover:no-underline">
        ← Apps
      </Link>

      <h2 className="text-xl font-semibold text-gray-900">{app.data?.name ?? '…'}</h2>

      {search.status && (
        <p
          className={`rounded border px-3 py-2 text-sm ${
            search.status === 'connected'
              ? 'border-green-200 bg-green-50 text-green-800'
              : 'border-red-200 bg-red-50 text-red-800'
          }`}
        >
          {search.status === 'connected'
            ? `Connected to ${search.provider}.`
            : `Could not connect to ${search.provider}: ${search.reason}`}
        </p>
      )}

      <section className="space-y-4">
        <h3 className="text-sm font-medium text-gray-700">Source</h3>
        <p className="text-sm text-gray-500">
          Connect the tracker this app opens issues on. GitLab and Bitbucket need this app's own OAuth
          application registered first.
        </p>

        <div className="grid gap-4 sm:grid-cols-3">
          {(providers.data?.providers ?? ALL_TRACKERS).map((tracker) => (
            <TrackerCard key={tracker} appId={appId} tracker={tracker} />
          ))}
        </div>
      </section>

      <section>
        <h3 className="mb-3 text-sm font-medium text-gray-700">Connections</h3>

        {connections.isLoading && <p className="text-sm text-gray-500">Loading…</p>}
        {connections.data && connections.data.length === 0 && (
          <p className="text-sm text-gray-500">No connections yet — connect a source above.</p>
        )}
        {connections.data && connections.data.length > 0 && (
          <ConnectionsTable appId={appId} connections={connections.data} />
        )}
      </section>

      <ProjectsSection appId={appId} />
      <KeysSection appId={appId} />
      <TestIssueSection appId={appId} />
    </Shell>
  )
}

function TrackerCard({ appId, tracker }: { appId: string; tracker: Tracker }) {
  const queryClient = useQueryClient()
  const needsOwnCredentials = tracker !== 'github'

  const status = useQuery({
    queryKey: ['apps', appId, 'credentials', tracker],
    queryFn: () => api.get<CredentialStatus>(`/apps/${appId}/providers/${tracker}/credentials`),
    enabled: needsOwnCredentials,
  })

  const connect = useMutation({
    mutationFn: () => api.get<{ url: string }>(`/apps/${appId}/connect/${tracker}`),
    onSuccess: (res) => {
      window.location.href = res.url
    },
  })

  const canConnect = !needsOwnCredentials || status.data?.configured

  return (
    <div className="space-y-2 rounded-lg border bg-white p-4">
      <h4 className="font-medium capitalize text-gray-900">{tracker}</h4>

      {needsOwnCredentials && !status.isLoading && !status.data?.configured && (
        <CredentialsForm
          appId={appId}
          tracker={tracker}
          onSaved={() => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'credentials', tracker] })}
        />
      )}

      {needsOwnCredentials && status.data?.configured && (
        <p className="text-xs text-gray-500">
          OAuth app registered (client ID <code>{status.data.client_id}</code>).
        </p>
      )}

      <button
        onClick={() => connect.mutate()}
        disabled={!canConnect || connect.isPending}
        className="w-full rounded bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
      >
        {connect.isPending ? 'Redirecting…' : 'Connect'}
      </button>
      {connect.isError && (
        <p className="text-xs text-red-600">
          {connect.error instanceof ApiError ? connect.error.message : 'could not start the connect flow'}
        </p>
      )}
    </div>
  )
}

function CredentialsForm({
  appId,
  tracker,
  onSaved,
}: {
  appId: string
  tracker: Tracker
  onSaved: () => void
}) {
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')

  const save = useMutation({
    mutationFn: () =>
      api.put(`/apps/${appId}/providers/${tracker}/credentials`, {
        client_id: clientId,
        client_secret: clientSecret,
      }),
    onSuccess: onSaved,
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
      className="space-y-2 rounded border border-dashed border-gray-300 p-2"
    >
      <input
        type="text"
        placeholder="Client ID"
        required
        value={clientId}
        onChange={(e) => setClientId(e.target.value)}
        className="w-full rounded border border-gray-300 px-2 py-1 text-xs"
      />
      <input
        type="password"
        placeholder="Client secret"
        required
        value={clientSecret}
        onChange={(e) => setClientSecret(e.target.value)}
        className="w-full rounded border border-gray-300 px-2 py-1 text-xs"
      />
      {save.isError && (
        <p className="text-xs text-red-600">
          {save.error instanceof ApiError ? save.error.message : 'could not save credentials'}
        </p>
      )}
      <button
        type="submit"
        disabled={save.isPending}
        className="w-full rounded border border-gray-900 px-2 py-1 text-xs font-medium text-gray-900 hover:bg-gray-100 disabled:opacity-50"
      >
        {save.isPending ? 'Saving…' : 'Save OAuth app'}
      </button>
    </form>
  )
}

function ConnectionsTable({ appId, connections }: { appId: string; connections: Connection[] }) {
  const queryClient = useQueryClient()
  const disconnect = useMutation({
    mutationFn: (connectionId: string) => api.delete(`/apps/${appId}/connections/${connectionId}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'connections'] }),
  })

  return (
    <table className="w-full overflow-hidden rounded-lg border bg-white text-sm">
      <thead className="bg-gray-100 text-left text-gray-600">
        <tr>
          <th className="px-4 py-2 font-medium">Tracker</th>
          <th className="px-4 py-2 font-medium">Account</th>
          <th className="px-4 py-2 font-medium">Status</th>
          <th className="px-4 py-2 font-medium" />
        </tr>
      </thead>
      <tbody>
        {connections.map((c) => (
          <tr key={c.id} className="border-t">
            <td className="px-4 py-2 capitalize text-gray-800">{c.tracker}</td>
            <td className="px-4 py-2 text-gray-800">{c.account_login}</td>
            <td className="px-4 py-2 text-gray-800">{c.suspended_at ? 'Suspended' : 'Active'}</td>
            <td className="px-4 py-2 text-right">
              <button
                onClick={() => disconnect.mutate(c.id)}
                disabled={disconnect.isPending}
                className="text-xs text-red-600 underline hover:no-underline disabled:opacity-50"
              >
                Disconnect
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function ProjectsSection({ appId }: { appId: string }) {
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState('')

  const linked = useQuery({
    queryKey: ['apps', appId, 'projects'],
    queryFn: () => api.get<AppProjectLink[]>(`/apps/${appId}/projects`),
  })
  const allProjects = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<Project[]>('/projects'),
  })

  const link = useMutation({
    mutationFn: (projectId: string) => api.put(`/apps/${appId}/projects/${projectId}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps', appId, 'projects'] })
      setSelected('')
    },
  })
  const unlink = useMutation({
    mutationFn: (projectId: string) => api.delete(`/apps/${appId}/projects/${projectId}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'projects'] }),
  })

  const linkableProjects = (allProjects.data ?? []).filter(
    (p) => !linked.data?.some((l) => l.id === p.id),
  )

  return (
    <section>
      <h3 className="mb-3 text-sm font-medium text-gray-700">Projects</h3>
      <p className="mb-3 text-sm text-gray-500">
        This app's issues go to the default repository of every project linked here.
      </p>

      {linked.data && linked.data.length > 0 && (
        <ul className="mb-3 divide-y rounded-lg border bg-white text-sm">
          {linked.data.map((p) => (
            <li key={p.id} className="flex items-center justify-between px-4 py-2">
              <span className="text-gray-800">{p.name}</span>
              <button
                onClick={() => unlink.mutate(p.id)}
                disabled={unlink.isPending}
                className="text-xs text-red-600 underline hover:no-underline disabled:opacity-50"
              >
                Unlink
              </button>
            </li>
          ))}
        </ul>
      )}
      {linked.data && linked.data.length === 0 && (
        <p className="mb-3 text-sm text-gray-500">No projects linked yet.</p>
      )}

      {linkableProjects.length > 0 && (
        <div className="flex gap-2">
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            className="rounded border border-gray-300 px-2 py-1.5 text-sm"
          >
            <option value="">Select a project…</option>
            {linkableProjects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <button
            onClick={() => selected && link.mutate(selected)}
            disabled={!selected || link.isPending}
            className="rounded bg-gray-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
          >
            Link
          </button>
        </div>
      )}
    </section>
  )
}

function KeysSection({ appId }: { appId: string }) {
  const queryClient = useQueryClient()
  const [lastSecret, setLastSecret] = useState<string | null>(null)

  const keys = useQuery({
    queryKey: ['apps', appId, 'keys'],
    queryFn: () => api.get<AppKey[]>(`/apps/${appId}/keys`),
  })

  const createKey = useMutation({
    mutationFn: () => api.post<AppKeyCreated>(`/apps/${appId}/keys`),
    onSuccess: (key) => {
      setLastSecret(key.secret)
      queryClient.invalidateQueries({ queryKey: ['apps', appId, 'keys'] })
    },
  })
  const revokeKey = useMutation({
    mutationFn: (keyId: string) => api.delete(`/apps/${appId}/keys/${keyId}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'keys'] }),
  })

  return (
    <section>
      <h3 className="mb-3 text-sm font-medium text-gray-700">SDK keys</h3>

      {lastSecret && (
        <p className="mb-3 rounded border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
          New key secret (shown once): <code className="font-mono">{lastSecret}</code>
        </p>
      )}

      {keys.data && keys.data.length > 0 && (
        <ul className="mb-3 divide-y rounded-lg border bg-white text-sm">
          {keys.data.map((k) => (
            <li key={k.id} className="flex items-center justify-between px-4 py-2">
              <span className="font-mono text-gray-800">
                {k.prefix} {k.revoked_at && <span className="text-gray-400">(revoked)</span>}
              </span>
              {!k.revoked_at && (
                <button
                  onClick={() => revokeKey.mutate(k.id)}
                  disabled={revokeKey.isPending}
                  className="text-xs text-red-600 underline hover:no-underline disabled:opacity-50"
                >
                  Revoke
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {keys.data && keys.data.length === 0 && <p className="mb-3 text-sm text-gray-500">No keys yet.</p>}

      <button
        onClick={() => createKey.mutate()}
        disabled={createKey.isPending}
        className="rounded bg-gray-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
      >
        {createKey.isPending ? 'Creating…' : 'Create key'}
      </button>
    </section>
  )
}

function TestIssueSection({ appId }: { appId: string }) {
  const targets = useQuery({
    queryKey: ['apps', appId, 'targets'],
    queryFn: () => api.get<Target[]>(`/apps/${appId}/targets`),
  })

  const sendTest = useMutation({
    mutationFn: () => api.post(`/apps/${appId}/test-issue`),
  })

  return (
    <section>
      <h3 className="mb-3 text-sm font-medium text-gray-700">Targets</h3>
      <p className="mb-3 text-sm text-gray-500">
        Where this app's issues would be opened right now, without opening any.
      </p>

      {targets.data && targets.data.length === 0 && (
        <p className="mb-3 text-sm text-gray-500">No targets yet — link a project with a default repository.</p>
      )}
      {targets.data && targets.data.length > 0 && (
        <ul className="mb-3 divide-y rounded-lg border bg-white text-sm">
          {targets.data.map((t) => (
            <li key={t.project_repository_id} className="px-4 py-2">
              <div className="flex items-center justify-between">
                <span className="text-gray-800">
                  {t.repository} <span className="text-gray-400">({t.tracker})</span>
                </span>
                <span className={t.connection_id ? 'text-xs text-green-700' : 'text-xs text-red-600'}>
                  {t.connection_id ? `via ${t.account_login}` : t.error}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}

      <button
        onClick={() => sendTest.mutate()}
        disabled={sendTest.isPending}
        className="rounded bg-gray-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
      >
        {sendTest.isPending ? 'Sending…' : 'Send test issue'}
      </button>
      {sendTest.isSuccess && <p className="mt-2 text-sm text-green-700">Test issue sent.</p>}
      {sendTest.isError && (
        <p className="mt-2 text-sm text-red-600">
          {sendTest.error instanceof ApiError ? sendTest.error.message : 'could not send test issue'}
        </p>
      )}
    </section>
  )
}
