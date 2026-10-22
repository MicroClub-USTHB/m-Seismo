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
import { McButton } from '../../../components/mc-button'
import { McInput } from '../../../components/mc-input'
import { McCard } from '../../../components/mc-card'
import { McBadge } from '../../../components/mc-badge'
import { McSeparator } from '../../../components/mc-separator'
import {
  McSelect,
  McSelectContent,
  McSelectItem,
  McSelectTrigger,
  McSelectValue,
} from '../../../components/mc-select'

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
      <div className="space-y-1">
        <Link to="/projects" className="text-sm text-muted-foreground hover:text-foreground hover:underline">
          ← Projects
        </Link>
        <h2 className="header-xs text-foreground">{app.data?.name ?? '…'}</h2>
      </div>

      {search.status && (
        <McCard
          className={
            search.status === 'connected'
              ? 'border-success/30 bg-success/10'
              : 'border-destructive/30 bg-destructive/10'
          }
        >
          <p className={`text-sm ${search.status === 'connected' ? 'text-success' : 'text-destructive'}`}>
            {search.status === 'connected'
              ? `Connected to ${search.provider}.`
              : `Could not connect to ${search.provider}: ${search.reason}`}
          </p>
        </McCard>
      )}

      <section className="space-y-4">
        <div>
          <h3 className="text-sm font-medium text-foreground">Source</h3>
          <p className="text-sm text-muted-foreground">
            Connect the tracker this app opens issues on. GitLab and Bitbucket need this app's own OAuth
            application registered first.
          </p>
        </div>

        <div className="grid gap-4 sm:grid-cols-3">
          {(providers.data?.providers ?? ALL_TRACKERS).map((tracker) => (
            <TrackerCard key={tracker} appId={appId} tracker={tracker} />
          ))}
        </div>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-medium text-foreground">Connections</h3>

        {connections.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {connections.data && connections.data.length === 0 && (
          <p className="text-sm text-muted-foreground">No connections yet — connect a source above.</p>
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
    <McCard className="gap-3">
      <h4 className="font-medium capitalize text-foreground">{tracker}</h4>

      {needsOwnCredentials && !status.isLoading && !status.data?.configured && (
        <CredentialsForm
          appId={appId}
          tracker={tracker}
          onSaved={() => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'credentials', tracker] })}
        />
      )}

      {needsOwnCredentials && status.data?.configured && (
        <p className="text-xs text-muted-foreground">
          OAuth app registered (client ID <code>{status.data.client_id}</code>).
        </p>
      )}

      <McButton
        onClick={() => connect.mutate()}
        disabled={!canConnect}
        isLoading={connect.isPending}
        className="w-full"
      >
        Connect
      </McButton>
      {connect.isError && (
        <p className="text-xs text-destructive">
          {connect.error instanceof ApiError ? connect.error.message : 'could not start the connect flow'}
        </p>
      )}
    </McCard>
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
      className="flex flex-col gap-2 rounded-lg border border-dashed border-border p-2"
    >
      <McInput
        placeholder="Client ID"
        required
        value={clientId}
        onChange={(e) => setClientId(e.target.value)}
      />
      <McInput
        type="password"
        placeholder="Client secret"
        required
        value={clientSecret}
        onChange={(e) => setClientSecret(e.target.value)}
      />
      {save.isError && (
        <p className="text-xs text-destructive">
          {save.error instanceof ApiError ? save.error.message : 'could not save credentials'}
        </p>
      )}
      <McButton type="submit" variant="secondary" isLoading={save.isPending} className="w-full">
        Save OAuth app
      </McButton>
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
    <McCard className="divide-y divide-border p-0">
      {connections.map((c) => (
        <div key={c.id} className="flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3 text-sm">
            <span className="capitalize text-foreground">{c.tracker}</span>
            <span className="text-muted-foreground">{c.account_login}</span>
            <McBadge
              variant={c.suspended_at ? 'destructive' : 'outline'}
              className={c.suspended_at ? undefined : 'text-success'}
            >
              {c.suspended_at ? 'Suspended' : 'Active'}
            </McBadge>
          </div>
          <McButton
            variant="link"
            destructive
            onClick={() => disconnect.mutate(c.id)}
            disabled={disconnect.isPending}
          >
            Disconnect
          </McButton>
        </div>
      ))}
    </McCard>
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
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium text-foreground">Projects</h3>
        <p className="text-sm text-muted-foreground">
          This app's issues go to the default repository of every project linked here.
        </p>
      </div>

      {linked.data && linked.data.length > 0 && (
        <McCard className="divide-y divide-border p-0">
          {linked.data.map((p) => (
            <div key={p.id} className="flex items-center justify-between px-4 py-2.5 text-sm">
              <span className="text-foreground">{p.name}</span>
              <McButton
                variant="link"
                destructive
                onClick={() => unlink.mutate(p.id)}
                disabled={unlink.isPending}
              >
                Unlink
              </McButton>
            </div>
          ))}
        </McCard>
      )}
      {linked.data && linked.data.length === 0 && (
        <p className="text-sm text-muted-foreground">No projects linked yet.</p>
      )}

      {linkableProjects.length > 0 && (
        <div className="flex gap-2">
          <McSelect value={selected} onValueChange={(value) => setSelected(value ?? '')}>
            <McSelectTrigger className="w-64">
              <McSelectValue placeholder="Select a project…" />
            </McSelectTrigger>
            <McSelectContent>
              {linkableProjects.map((p) => (
                <McSelectItem key={p.id} value={p.id}>
                  {p.name}
                </McSelectItem>
              ))}
            </McSelectContent>
          </McSelect>
          <McButton
            onClick={() => selected && link.mutate(selected)}
            disabled={!selected}
            isLoading={link.isPending}
          >
            Link
          </McButton>
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
    <section className="space-y-3">
      <h3 className="text-sm font-medium text-foreground">SDK keys</h3>

      {lastSecret && (
        <McCard className="border-warning/30 bg-warning/10">
          <p className="text-sm text-warning">
            New key secret (shown once): <code className="font-mono">{lastSecret}</code>
          </p>
        </McCard>
      )}

      {keys.data && keys.data.length > 0 && (
        <McCard className="divide-y divide-border p-0">
          {keys.data.map((k) => (
            <div key={k.id} className="flex items-center justify-between px-4 py-2.5 text-sm">
              <span className="font-mono text-foreground">
                {k.prefix} {k.revoked_at && <span className="text-muted-foreground">(revoked)</span>}
              </span>
              {!k.revoked_at && (
                <McButton
                  variant="link"
                  destructive
                  onClick={() => revokeKey.mutate(k.id)}
                  disabled={revokeKey.isPending}
                >
                  Revoke
                </McButton>
              )}
            </div>
          ))}
        </McCard>
      )}
      {keys.data && keys.data.length === 0 && <p className="text-sm text-muted-foreground">No keys yet.</p>}

      <McButton onClick={() => createKey.mutate()} isLoading={createKey.isPending}>
        Create key
      </McButton>
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
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium text-foreground">Targets</h3>
        <p className="text-sm text-muted-foreground">
          Where this app's issues would be opened right now, without opening any.
        </p>
      </div>

      {targets.data && targets.data.length === 0 && (
        <p className="text-sm text-muted-foreground">No targets yet — link a project with a default repository.</p>
      )}
      {targets.data && targets.data.length > 0 && (
        <McCard className="divide-y divide-border p-0">
          {targets.data.map((t) => (
            <div key={t.project_repository_id} className="flex items-center justify-between px-4 py-2.5 text-sm">
              <span className="text-foreground">
                {t.repository} <span className="text-muted-foreground">({t.tracker})</span>
              </span>
              <McBadge
                variant={t.connection_id ? 'outline' : 'destructive'}
                className={t.connection_id ? 'text-success' : undefined}
              >
                {t.connection_id ? `via ${t.account_login}` : t.error}
              </McBadge>
            </div>
          ))}
        </McCard>
      )}

      <McSeparator />

      <McButton onClick={() => sendTest.mutate()} isLoading={sendTest.isPending}>
        Send test issue
      </McButton>
      {sendTest.isSuccess && <p className="text-sm text-success">Test issue sent.</p>}
      {sendTest.isError && (
        <p className="text-sm text-destructive">
          {sendTest.error instanceof ApiError ? sendTest.error.message : 'could not send test issue'}
        </p>
      )}
    </section>
  )
}
