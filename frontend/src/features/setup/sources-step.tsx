import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type AppKey, type AppKeyCreated, type CrashProviderConnection, type CrashlyticsStatus, type ProjectApp } from '../../lib/api'
import { McButton } from '../../components/mc-button'
import { McInput } from '../../components/mc-input'
import { McCard } from '../../components/mc-card'
import { McBadge } from '../../components/mc-badge'

const errorText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

export function SourcesStep({ projectId, apps }: { projectId: string; apps: ProjectApp[] }) {
  const [appId, setAppId] = useState(apps[0]?.id ?? '')
  return (
    <section className="space-y-8" data-testid="step-sources">
      <p className="text-sm text-muted-foreground">
        Link where crashes and server logs come from, so an issue carries them. Everything here is optional; a project without sources still opens issues from reports.
      </p>
      <CrashlyticsPanel projectId={projectId} />
      <div className="flex items-center gap-2">
        <label htmlFor="source-app" className="text-sm font-medium text-muted-foreground">
          Sources for app
        </label>
        <select
          id="source-app"
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
      {appId && <SentryPanel key={`s-${appId}`} appId={appId} />}
      {appId && <CoordinatorPanel key={`c-${appId}`} appId={appId} />}
    </section>
  )
}

function SentryPanel({ appId }: { appId: string }) {
  const queryClient = useQueryClient()
  const key = ['apps', appId, 'crash-provider']
  const current = useQuery({
    queryKey: key,
    queryFn: async () => {
      try {
        return await api.get<CrashProviderConnection>(`/apps/${appId}/crash-provider`)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
  const [baseUrl, setBaseUrl] = useState('https://sentry.io')
  const [organization, setOrganization] = useState('')
  const [project, setProject] = useState('')
  const [token, setToken] = useState('')

  const save = useMutation({
    mutationFn: () =>
      api.put(`/apps/${appId}/crash-provider`, {
        provider: 'sentry',
        sentry: { base_url: baseUrl, organization, project, auth_token: token },
      }),
    onSuccess: () => {
      setToken('')
      queryClient.invalidateQueries({ queryKey: key })
    },
  })
  const remove = useMutation({
    mutationFn: () => api.delete(`/apps/${appId}/crash-provider`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  })

  const sentry = current.data?.provider === 'sentry' ? current.data.sentry : undefined
  return (
    <McCard className="gap-3" data-testid="sentry-panel">
      <div className="flex items-center gap-2">
        <h4 className="font-medium text-foreground">Sentry</h4>
        {sentry && <McBadge>linked</McBadge>}
      </div>
      <p className="text-sm text-muted-foreground">
        Reports that carry a Sentry event ID get the resolved stack trace and a link to the event. Use an auth token that can read the project's events. It is stored encrypted and never shown again.
      </p>
      {sentry && (
        <p className="text-sm text-foreground">
          {sentry.organization}/{sentry.project} on {sentry.base_url}
        </p>
      )}
      <form
        className="grid gap-3 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <McInput label="Sentry URL" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
        <McInput label="Organization slug" required value={organization} onChange={(e) => setOrganization(e.target.value)} />
        <McInput label="Project slug" required value={project} onChange={(e) => setProject(e.target.value)} />
        <McInput label="Auth token" type="password" required value={token} onChange={(e) => setToken(e.target.value)} />
        <div className="flex gap-2 sm:col-span-2">
          <McButton type="submit" isLoading={save.isPending}>
            {sentry ? 'Replace' : 'Link Sentry'}
          </McButton>
          {sentry && (
            <McButton type="button" variant="secondary" onClick={() => remove.mutate()}>
              Unlink
            </McButton>
          )}
        </div>
      </form>
      {(save.isError || remove.isError) && <p className="text-sm text-destructive">{errorText(save.error ?? remove.error, 'request failed')}</p>}
    </McCard>
  )
}

function CrashlyticsPanel({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient()
  const key = ['projects', projectId, 'crashlytics']
  const status = useQuery({
    queryKey: key,
    queryFn: () => api.get<CrashlyticsStatus>(`/projects/${projectId}/crashlytics`),
    retry: false,
  })
  const [apps, setApps] = useState('')

  const connect = useMutation({
    mutationFn: () => api.post<{ url: string }>(`/projects/${projectId}/crashlytics/connect`),
    // Google's consent page opens in its own tab; this one stays on the wizard.
    onSuccess: (r) => window.open(r.url, '_blank', 'noopener'),
  })
  const saveApps = useMutation({
    mutationFn: () =>
      api.put(`/projects/${projectId}/crashlytics/apps`, {
        apps: apps
          .split('\n')
          .map((a) => a.trim())
          .filter(Boolean),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  })

  const notEnabled = status.error instanceof ApiError && status.error.status === 404
  return (
    <McCard className="gap-3" data-testid="crashlytics-panel">
      <div className="flex items-center gap-2">
        <h4 className="font-medium text-foreground">Firebase Crashlytics</h4>
        {status.data?.connected && <McBadge>connected</McBadge>}
      </div>
      {notEnabled ? (
        <p className="text-sm text-muted-foreground">Crashlytics is not enabled on this server. The operator turns it on with the crash source setting.</p>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            Sign in with the Google account that can read the Firebase project. Crashlytics belongs to the whole project, not to one app.
          </p>
          {status.data?.connected && <p className="text-sm text-foreground">Signed in as {status.data.google_email}</p>}
          <div className="flex gap-2">
            <McButton onClick={() => connect.mutate()} isLoading={connect.isPending}>
              {status.data?.connected ? 'Reconnect' : 'Connect Google account'}
            </McButton>
            <McButton variant="secondary" onClick={() => status.refetch()}>
              Refresh status
            </McButton>
          </div>
          {status.data?.connected && (
            <form
              className="space-y-2"
              onSubmit={(e) => {
                e.preventDefault()
                saveApps.mutate()
              }}
            >
              <label className="text-sm font-medium text-muted-foreground" htmlFor="fb-apps">
                Firebase apps, one per line (projects/&lt;number&gt;/apps/&lt;app id&gt;)
              </label>
              <textarea
                id="fb-apps"
                rows={3}
                value={apps}
                placeholder={(status.data.apps ?? []).join('\n') || 'projects/123456/apps/1:123456:android:abcd'}
                onChange={(e) => setApps(e.target.value)}
                className="w-full rounded-md border border-border bg-background p-2 text-sm"
              />
              <McButton type="submit" isLoading={saveApps.isPending}>
                Save apps
              </McButton>
              {(status.data.apps ?? []).length > 0 && (
                <p className="text-xs text-muted-foreground">Saved: {(status.data.apps ?? []).join(', ')}</p>
              )}
            </form>
          )}
          {(connect.isError || saveApps.isError) && <p className="text-sm text-destructive">{errorText(connect.error ?? saveApps.error, 'request failed')}</p>}
        </>
      )}
    </McCard>
  )
}

function CoordinatorPanel({ appId }: { appId: string }) {
  const queryClient = useQueryClient()
  const key = ['apps', appId, 'coordinator-keys']
  const [secret, setSecret] = useState<string | null>(null)
  const keys = useQuery({ queryKey: key, queryFn: () => api.get<AppKey[]>(`/apps/${appId}/coordinator-keys`) })
  const create = useMutation({
    mutationFn: () => api.post<AppKeyCreated>(`/apps/${appId}/coordinator-keys`),
    onSuccess: (k) => {
      setSecret(k.secret)
      queryClient.invalidateQueries({ queryKey: key })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.delete(`/apps/${appId}/coordinator-keys/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
  })
  const active = (keys.data ?? []).filter((k) => !k.revoked_at)
  return (
    <McCard className="gap-3" data-testid="coordinator-panel">
      <div className="flex items-center gap-2">
        <h4 className="font-medium text-foreground">Server logs (coordinator)</h4>
        {active.length > 0 && <McBadge>{active.length} key{active.length > 1 ? 's' : ''}</McBadge>}
      </div>
      <p className="text-sm text-muted-foreground">
        Run the log coordinator next to your servers with a coordinator key (<code>mco_…</code>). Its logs around each report are added to the issue.
      </p>
      <ul className="space-y-1 text-sm">
        {active.map((k) => (
          <li key={k.id} className="flex items-center gap-3">
            <code>{k.prefix}…</code>
            <McButton variant="tertiary" size="sm" onClick={() => revoke.mutate(k.id)}>
              Revoke
            </McButton>
          </li>
        ))}
      </ul>
      {secret && (
        <div className="rounded-md border border-success/30 bg-success/10 p-3 text-sm">
          Copy this key now, it is shown once: <code data-testid="coordinator-secret">{secret}</code>
        </div>
      )}
      <div>
        <McButton onClick={() => create.mutate()} isLoading={create.isPending}>
          Create coordinator key
        </McButton>
      </div>
      {(create.isError || revoke.isError) && <p className="text-sm text-destructive">{errorText(create.error ?? revoke.error, 'request failed')}</p>}
    </McCard>
  )
}
