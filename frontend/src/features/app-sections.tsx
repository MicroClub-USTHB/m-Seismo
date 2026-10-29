import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  api,
  ApiError,
  type AppKey,
  type AppKeyCreated,
  type CredentialStatus,
  type Target,
  type Tracker,
} from '../lib/api'
import { McButton } from '../components/mc-button'
import { McInput } from '../components/mc-input'
import { McCard } from '../components/mc-card'
import { McBadge } from '../components/mc-badge'
import { McSeparator } from '../components/mc-separator'

export const RETURN_KEY = 'm-seismo.connect-return'

export function TrackerCard({ appId, tracker, returnTo }: { appId: string; tracker: Tracker; returnTo?: string }) {
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
      // The backend sends the browser back to the app's connections page;
      // remember where the person came from so that page can return them.
      if (returnTo) {
        try {
          sessionStorage.setItem(RETURN_KEY, returnTo)
        } catch {
          /* private mode: the person lands on the connections page instead */
        }
      }
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

export function CredentialsForm({
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


export function KeysSection({ appId }: { appId: string }) {
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

export function TestIssueSection({ appId }: { appId: string }) {
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
