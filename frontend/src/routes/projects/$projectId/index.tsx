import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError, type Platform, type Project, type ProjectApp } from '../../../lib/api'
import { getStoredOwner } from '../../../lib/auth'
import { Shell } from '../../../lib/Shell'
import { CreateAppForm, PLATFORM_LABEL } from '../../../features/create-app'
import { McCard, McCardHeader } from '../../../components/mc-card'
import { McBadge } from '../../../components/mc-badge'
import { McDataTable, type McDataTableColumnDef } from '../../../components/mc-data-table'

export const Route = createFileRoute('/projects/$projectId/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: ProjectDetailPage,
})

const columns: McDataTableColumnDef<ProjectApp>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: (ctx) => (
      <Link
        to="/apps/$appId"
        params={{ appId: ctx.row.original.id }}
        className="font-medium text-foreground hover:underline"
      >
        {ctx.getValue() as string}
      </Link>
    ),
  },
  {
    accessorKey: 'platform',
    header: 'Platform',
    cell: (ctx) => <McBadge>{PLATFORM_LABEL[ctx.getValue() as Platform]}</McBadge>,
  },
  { accessorKey: 'bundle_id', header: 'Bundle ID' },
]

function ProjectDetailPage() {
  const { projectId } = Route.useParams()

  const project = useQuery({
    queryKey: ['projects', projectId],
    queryFn: () => api.get<Project>(`/projects/${projectId}`),
  })

  const apps = useQuery({
    queryKey: ['projects', projectId, 'apps'],
    queryFn: () => api.get<ProjectApp[]>(`/projects/${projectId}/apps`),
  })

  return (
    <Shell>
      <div className="space-y-1">
        <Link to="/projects" className="text-sm text-muted-foreground hover:text-foreground hover:underline">
          ← Projects
        </Link>
        <h2 className="header-xs text-foreground">{project.data?.name ?? '…'}</h2>
      </div>

      <McCard direction="row" className="items-center justify-between">
        <McCardHeader
          title="Apps"
          description="The web, iOS and Android builds that share this project's repositories."
        />
        <div className="flex items-center gap-2">
          <Link
            to="/projects/$projectId/setup"
            params={{ projectId }}
            search={{ step: 'apps' }}
            className="text-sm font-medium text-primary hover:underline"
          >
            Project setup
          </Link>
          <CreateAppForm projectId={projectId} />
        </div>
      </McCard>

      {apps.isError && (
        <p className="text-sm text-destructive">
          {apps.error instanceof ApiError ? apps.error.message : 'could not load apps'}
        </p>
      )}

      <McDataTable
        columns={columns}
        data={apps.data ?? []}
        getRowId={(app) => app.id}
        empty={
          <p className="p-6 text-center text-sm text-muted-foreground">
            No apps yet — create one above.
          </p>
        }
      />
    </Shell>
  )
}
