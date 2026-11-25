import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, ALL_TRACKERS, type Connection, type ProjectApp, type RepositoryLink, type TrackerRepository } from '../../lib/api'
import { McButton } from '../../components/mc-button'
import { McBadge } from '../../components/mc-badge'
import { TrackerCard } from '../app-sections'

const errorText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

export function RepositoryStep({ projectId, apps }: { projectId: string; apps: ProjectApp[] }) {
  const queryClient = useQueryClient()
  const [appId, setAppId] = useState(apps[0]?.id ?? '')
  const returnTo = `/projects/${projectId}/setup?step=repository`

  const links = useQuery({
    queryKey: ['projects', projectId, 'repositories'],
    queryFn: () => api.get<RepositoryLink[]>(`/projects/${projectId}/repositories`),
  })
  const connections = useQuery({
    queryKey: ['apps', appId, 'connections'],
    queryFn: () => api.get<Connection[]>(`/apps/${appId}/connections`),
    enabled: !!appId,
  })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['projects', projectId, 'repositories'] })

  const setDefault = useMutation({
    mutationFn: (id: string) => api.put(`/projects/${projectId}/repositories/${id}/default`),
    onSuccess: refresh,
  })
  const unlink = useMutation({
    mutationFn: (id: string) => api.delete(`/projects/${projectId}/repositories/${id}`),
    onSuccess: refresh,
  })

  return (
    <section className="space-y-6" data-testid="step-repository">
      <div>
        <h3 className="text-sm font-medium text-foreground">Linked repositories</h3>
        <p className="text-sm text-muted-foreground">Issues are opened here. The default repository gets every report no rule claims.</p>
        {links.data?.length === 0 && <p className="mt-2 text-sm text-muted-foreground">None yet. Connect a tracker below and link one.</p>}
        <ul className="mt-2 space-y-2">
          {links.data?.map((l) => (
            <li key={l.id} className="flex items-center gap-3 rounded-md border border-border p-3 text-sm">
              <span className="font-medium text-foreground">{l.repository}</span>
              <McBadge>{l.tracker}</McBadge>
              {l.is_default ? (
                <McBadge>default</McBadge>
              ) : (
                <McButton variant="secondary" size="sm" onClick={() => setDefault.mutate(l.id)}>
                  Make default
                </McButton>
              )}
              <McButton variant="tertiary" size="sm" onClick={() => unlink.mutate(l.id)}>
                Unlink
              </McButton>
            </li>
          ))}
        </ul>
        {(setDefault.isError || unlink.isError) && (
          <p className="text-sm text-destructive">{errorText(setDefault.error ?? unlink.error, 'request failed')}</p>
        )}
      </div>

      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <label htmlFor="repo-app" className="text-sm font-medium text-muted-foreground">
            Connect through app
          </label>
          <select
            id="repo-app"
            value={appId}
            onChange={(e) => setAppId(e.target.value)}
            className="rounded-md border border-border bg-background px-2 py-1 text-sm"
          >
            {apps.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </div>
        <p className="text-sm text-muted-foreground">
          A tracker account belongs to an app. Repositories you link here can be used by every app of this project, through that app's own connection.
        </p>

        {connections.data?.map((c) => (
          <ConnectionRepositories key={c.id} appId={appId} connection={c} projectId={projectId} linked={links.data ?? []} onLinked={refresh} />
        ))}

        <h4 className="pt-2 text-sm font-medium text-foreground">Connect a tracker</h4>
        <div className="grid gap-4 sm:grid-cols-3">
          {ALL_TRACKERS.map((t) => (
            <TrackerCard key={t} appId={appId} tracker={t} returnTo={returnTo} />
          ))}
        </div>
      </div>
    </section>
  )
}

function ConnectionRepositories({
  appId,
  connection,
  projectId,
  linked,
  onLinked,
}: {
  appId: string
  connection: Connection
  projectId: string
  linked: RepositoryLink[]
  onLinked: () => void
}) {
  const [filter, setFilter] = useState('')
  const repos = useQuery({
    queryKey: ['apps', appId, 'connections', connection.id, 'repositories'],
    queryFn: () => api.get<TrackerRepository[]>(`/apps/${appId}/connections/${connection.id}/repositories`),
  })
  const link = useMutation({
    mutationFn: (full: string) => api.post(`/projects/${projectId}/repositories`, { tracker: connection.tracker, repository: full }),
    onSuccess: onLinked,
  })
  const isLinked = (full: string) => linked.some((l) => l.tracker === connection.tracker && l.repository.toLowerCase() === full.toLowerCase())
  const shown = (repos.data ?? []).filter((r) => r.full_name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div className="rounded-md border border-border p-3" data-testid={`connection-${connection.tracker}`}>
      <div className="flex items-center gap-2 text-sm">
        <McBadge>{connection.tracker}</McBadge>
        <span className="font-medium text-foreground">{connection.account_login}</span>
        <input
          aria-label="Filter repositories"
          placeholder="Filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="ml-auto rounded-md border border-border bg-background px-2 py-1 text-sm"
        />
      </div>
      {repos.isLoading && <p className="mt-2 text-sm text-muted-foreground">Loading repositories…</p>}
      {repos.isError && <p className="mt-2 text-sm text-destructive">{errorText(repos.error, 'could not list repositories')}</p>}
      <ul className="mt-2 max-h-64 space-y-1 overflow-auto">
        {shown.map((r) => (
          <li key={r.full_name} className="flex items-center gap-3 text-sm">
            <span className="text-foreground">{r.full_name}</span>
            {r.private && <McBadge>private</McBadge>}
            <McButton
              variant="secondary"
              size="sm"
              className="ml-auto"
              disabled={isLinked(r.full_name)}
              isLoading={link.isPending && link.variables === r.full_name}
              onClick={() => link.mutate(r.full_name)}
            >
              {isLinked(r.full_name) ? 'Linked' : 'Link'}
            </McButton>
          </li>
        ))}
      </ul>
      {link.isError && <p className="mt-2 text-sm text-destructive">{errorText(link.error, 'could not link the repository')}</p>}
    </div>
  )
}
