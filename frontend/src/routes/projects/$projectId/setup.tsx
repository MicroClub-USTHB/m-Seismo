import { createFileRoute, Link, redirect, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api, type Project, type ProjectApp, type RepositoryLink, type RoutingRule } from '../../../lib/api'
import { getStoredOwner } from '../../../lib/auth'
import { Shell } from '../../../lib/Shell'
import { McButton } from '../../../components/mc-button'
import { McCard } from '../../../components/mc-card'
import { AppsStep } from '../../../features/setup/apps-step'
import { RepositoryStep } from '../../../features/setup/repository-step'
import { SourcesStep } from '../../../features/setup/sources-step'
import { RoutingStep } from '../../../features/setup/routing-step'
import { DoneStep } from '../../../features/setup/done-step'

const STEPS = [
  { id: 'apps', title: 'Apps' },
  { id: 'repository', title: 'Repository' },
  { id: 'sources', title: 'Log sources' },
  { id: 'routing', title: 'Categorization' },
  { id: 'done', title: 'Keys and test' },
] as const
type StepId = (typeof STEPS)[number]['id']

interface SetupSearch {
  step: StepId
  provider?: string
  status?: string
  reason?: string
}

export const Route = createFileRoute('/projects/$projectId/setup')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  validateSearch: (s: Record<string, unknown>): SetupSearch => ({
    step: STEPS.some((x) => x.id === s.step) ? (s.step as StepId) : 'apps',
    provider: typeof s.provider === 'string' && s.provider ? s.provider : undefined,
    status: typeof s.status === 'string' && s.status ? s.status : undefined,
    reason: typeof s.reason === 'string' && s.reason ? s.reason : undefined,
  }),
  component: SetupPage,
})

function SetupPage() {
  const { projectId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const project = useQuery({ queryKey: ['projects', projectId], queryFn: () => api.get<Project>(`/projects/${projectId}`) })
  const apps = useQuery({ queryKey: ['projects', projectId, 'apps'], queryFn: () => api.get<ProjectApp[]>(`/projects/${projectId}/apps`) })
  const repos = useQuery({
    queryKey: ['projects', projectId, 'repositories'],
    queryFn: () => api.get<RepositoryLink[]>(`/projects/${projectId}/repositories`),
  })
  const rules = useQuery({
    queryKey: ['projects', projectId, 'routing-rules'],
    queryFn: () => api.get<RoutingRule[]>(`/projects/${projectId}/routing-rules`),
  })

  const appList = apps.data ?? []
  const hasApps = appList.length > 0
  const hasRepo = (repos.data?.length ?? 0) > 0
  // A step needs what the earlier ones produce.
  const blocked: Record<StepId, string | null> = {
    apps: null,
    repository: hasApps ? null : 'Create an app first.',
    sources: hasApps ? null : 'Create an app first.',
    routing: !hasApps ? 'Create an app first.' : !hasRepo ? 'Link a repository first.' : null,
    done: !hasApps ? 'Create an app first.' : null,
  }
  const index = STEPS.findIndex((s) => s.id === search.step)
  const go = (step: StepId) => navigate({ search: { step } })
  const complete: Record<StepId, boolean> = {
    apps: hasApps,
    repository: hasRepo,
    sources: false,
    routing: (rules.data?.length ?? 0) > 0,
    done: false,
  }

  return (
    <Shell>
      <div className="space-y-1">
        <Link to="/projects/$projectId" params={{ projectId }} className="text-sm text-muted-foreground hover:text-foreground hover:underline">
          ← {project.data?.name ?? 'Project'}
        </Link>
        <h2 className="header-xs text-foreground">Set up {project.data?.name}</h2>
      </div>

      <nav aria-label="Setup steps" className="flex flex-wrap gap-2">
        {STEPS.map((s, i) => (
          <button
            key={s.id}
            type="button"
            data-testid={`tab-${s.id}`}
            aria-current={s.id === search.step ? 'step' : undefined}
            onClick={() => go(s.id)}
            className={`rounded-full border px-3 py-1 text-sm ${
              s.id === search.step ? 'border-primary bg-primary text-primary-foreground' : 'border-border text-muted-foreground hover:text-foreground'
            }`}
          >
            {i + 1}. {s.title}
            {complete[s.id] ? ' ✓' : ''}
          </button>
        ))}
      </nav>

      {search.status && (
        <McCard className={search.status === 'connected' ? 'border-success/30 bg-success/10' : 'border-destructive/30 bg-destructive/10'}>
          <p className={`text-sm ${search.status === 'connected' ? 'text-success' : 'text-destructive'}`}>
            {search.status === 'connected' ? `Connected to ${search.provider}.` : `Could not connect to ${search.provider}: ${search.reason}`}
          </p>
        </McCard>
      )}

      {blocked[search.step] ? (
        <p className="text-sm text-muted-foreground" data-testid="blocked">
          {blocked[search.step]}
        </p>
      ) : (
        <>
          {search.step === 'apps' && <AppsStep projectId={projectId} />}
          {search.step === 'repository' && <RepositoryStep projectId={projectId} apps={appList} />}
          {search.step === 'sources' && <SourcesStep projectId={projectId} apps={appList} />}
          {search.step === 'routing' && <RoutingStep projectId={projectId} apps={appList} />}
          {search.step === 'done' && <DoneStep apps={appList} />}
        </>
      )}

      <div className="flex justify-between pt-2">
        <McButton variant="secondary" disabled={index === 0} onClick={() => go(STEPS[index - 1].id)}>
          Back
        </McButton>
        {index < STEPS.length - 1 ? (
          <McButton data-testid="next" disabled={!!blocked[search.step] || (search.step === 'apps' && !hasApps)} onClick={() => go(STEPS[index + 1].id)}>
            Next
          </McButton>
        ) : (
          <Link to="/projects/$projectId" params={{ projectId }} className="text-sm font-medium text-primary hover:underline">
            Finish
          </Link>
        )}
      </div>
    </Shell>
  )
}
