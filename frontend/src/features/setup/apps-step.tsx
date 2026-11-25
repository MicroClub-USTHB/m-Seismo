import { useQuery } from '@tanstack/react-query'
import { api, type ProjectApp } from '../../lib/api'
import { McCard, McCardHeader } from '../../components/mc-card'
import { McBadge } from '../../components/mc-badge'
import { CreateAppForm, PLATFORM_LABEL } from '../create-app'

export function AppsStep({ projectId }: { projectId: string }) {
  const apps = useQuery({
    queryKey: ['projects', projectId, 'apps'],
    queryFn: () => api.get<ProjectApp[]>(`/projects/${projectId}/apps`),
  })
  return (
    <section className="space-y-4" data-testid="step-apps">
      <McCard direction="row" className="items-center justify-between">
        <McCardHeader
          title="Apps"
          description="Each app is a web, iOS or Android build. Every app gets its own keys and its own tracker and log connections."
        />
        <CreateAppForm projectId={projectId} />
      </McCard>
      {apps.data?.length === 0 && <p className="text-sm text-muted-foreground">Create at least one app to continue.</p>}
      <ul className="space-y-2">
        {apps.data?.map((a) => (
          <li key={a.id} className="flex items-center gap-3 rounded-md border border-border p-3 text-sm">
            <span className="font-medium text-foreground">{a.name}</span>
            <McBadge>{PLATFORM_LABEL[a.platform]}</McBadge>
            <code className="text-muted-foreground">{a.bundle_id}</code>
          </li>
        ))}
      </ul>
    </section>
  )
}
