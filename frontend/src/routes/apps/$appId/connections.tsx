import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import {
  api,
  ALL_TRACKERS,
  type App,
  type AppProjectLink,
  type Connection,
  type Project,
  type Tracker,
} from '../../../lib/api'
import { getStoredOwner } from '../../../lib/auth'
import { Shell } from '../../../lib/Shell'
import { McButton } from '../../../components/mc-button'
import { McCard } from '../../../components/mc-card'
import { McBadge } from '../../../components/mc-badge'
import { KeysSection, RETURN_KEY, TestIssueSection, TrackerCard } from '../../../features/app-sections'
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

  // A connect flow started from the project setup returns there.
  useEffect(() => {
    if (!search.status) return
    let back: string | null = null
    try {
      back = sessionStorage.getItem(RETURN_KEY)
      sessionStorage.removeItem(RETURN_KEY)
    } catch {
      /* ignore */
    }
    if (back && back.startsWith('/')) {
      const q = new URLSearchParams({ provider: search.provider ?? '', status: search.status, reason: search.reason ?? '' })
      window.location.replace(`${back}${back.includes('?') ? '&' : '?'}${q}`)
    }
  }, [search.status, search.provider, search.reason])

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

