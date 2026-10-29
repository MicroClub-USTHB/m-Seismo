import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Platform, type Project, type ProjectApp } from '../../../lib/api'
import { getStoredOwner } from '../../../lib/auth'
import { Shell } from '../../../lib/Shell'
import { McButton } from '../../../components/mc-button'
import { McInput } from '../../../components/mc-input'
import { McCard, McCardHeader } from '../../../components/mc-card'
import { McBadge } from '../../../components/mc-badge'
import { McDataTable, type McDataTableColumnDef } from '../../../components/mc-data-table'
import {
  McDialog,
  McDialogContent,
  McDialogFooter,
  McDialogHeader,
  McDialogTitle,
  McDialogTrigger,
} from '../../../components/mc-dialog'
import {
  McSelect,
  McSelectContent,
  McSelectItem,
  McSelectTrigger,
  McSelectValue,
} from '../../../components/mc-select'

export const Route = createFileRoute('/projects/$projectId/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: ProjectDetailPage,
})

const PLATFORMS: Platform[] = ['web', 'ios', 'android']

const PLATFORM_LABEL: Record<Platform, string> = {
  web: 'Web',
  ios: 'iOS',
  android: 'Android',
}

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
        <CreateAppForm projectId={projectId} />
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

function CreateAppForm({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [platform, setPlatform] = useState<Platform>('web')
  const [bundleId, setBundleId] = useState('')

  const createApp = useMutation({
    mutationFn: () =>
      api.post<ProjectApp>(`/projects/${projectId}/apps`, { name, platform, bundle_id: bundleId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects', projectId, 'apps'] })
      setName('')
      setBundleId('')
      setOpen(false)
    },
  })

  return (
    <McDialog open={open} onOpenChange={setOpen}>
      <McDialogTrigger render={<McButton>Create app</McButton>} />
      <McDialogContent>
        <McDialogHeader>
          <McDialogTitle>Create app</McDialogTitle>
        </McDialogHeader>

        <form
          id="create-project-app-form"
          onSubmit={(e) => {
            e.preventDefault()
            createApp.mutate()
          }}
          className="flex flex-col gap-3"
        >
          <McInput label="Name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />

          <div className="flex flex-col gap-1.5">
            <label className="text-sm font-medium text-muted-foreground">Platform</label>
            <McSelect value={platform} onValueChange={(value) => setPlatform(value as Platform)}>
              <McSelectTrigger className="w-full">
                <McSelectValue />
              </McSelectTrigger>
              <McSelectContent>
                {PLATFORMS.map((p) => (
                  <McSelectItem key={p} value={p}>
                    {PLATFORM_LABEL[p]}
                  </McSelectItem>
                ))}
              </McSelectContent>
            </McSelect>
          </div>

          <McInput
            label="Bundle ID"
            description="e.g. com.example.shop"
            required
            value={bundleId}
            onChange={(e) => setBundleId(e.target.value)}
          />

          {createApp.isError && (
            <p className="text-sm text-destructive">
              {createApp.error instanceof ApiError ? createApp.error.message : 'could not create app'}
            </p>
          )}
        </form>

        <McDialogFooter>
          <McButton variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </McButton>
          <McButton type="submit" form="create-project-app-form" isLoading={createApp.isPending}>
            Create
          </McButton>
        </McDialogFooter>
      </McDialogContent>
    </McDialog>
  )
}
