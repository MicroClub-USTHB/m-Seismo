import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Project } from '../../lib/api'
import { getStoredOwner } from '../../lib/auth'
import { Shell } from '../../lib/Shell'
import { McButton } from '../../components/mc-button'
import { McInput } from '../../components/mc-input'
import { McCard, McCardHeader } from '../../components/mc-card'
import { McDataTable, type McDataTableColumnDef } from '../../components/mc-data-table'
import {
  McDialog,
  McDialogContent,
  McDialogFooter,
  McDialogHeader,
  McDialogTitle,
  McDialogTrigger,
} from '../../components/mc-dialog'

export const Route = createFileRoute('/projects/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: ProjectsPage,
})

const columns: McDataTableColumnDef<Project>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: (ctx) => (
      <Link
        to="/projects/$projectId"
        params={{ projectId: ctx.row.original.id }}
        className="font-medium text-foreground hover:underline"
      >
        {ctx.getValue() as string}
      </Link>
    ),
  },
  { accessorKey: 'slug', header: 'Slug' },
  {
    accessorKey: 'created_at',
    header: 'Created',
    cell: (ctx) => new Date(ctx.getValue() as string).toLocaleString(),
  },
]

function ProjectsPage() {
  const projects = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.get<Project[]>('/projects'),
  })

  return (
    <Shell>
      <McCard direction="row" className="items-center justify-between">
        <McCardHeader
          title="Projects"
          description="Group your web, iOS and Android apps by product — each project's repositories are shared across the apps linked to it."
        />
        <CreateProjectForm />
      </McCard>

      {projects.isError && (
        <p className="text-sm text-destructive">
          {projects.error instanceof ApiError ? projects.error.message : 'could not load projects'}
        </p>
      )}

      <McDataTable
        columns={columns}
        data={projects.data ?? []}
        getRowId={(project) => project.id}
        empty={<p className="p-6 text-center text-sm text-muted-foreground">No projects yet.</p>}
      />
    </Shell>
  )
}

function CreateProjectForm() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')

  const createProject = useMutation({
    mutationFn: () => api.post<Project>('/projects', { name }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setName('')
      setOpen(false)
    },
  })

  return (
    <McDialog open={open} onOpenChange={setOpen}>
      <McDialogTrigger render={<McButton>Create project</McButton>} />
      <McDialogContent>
        <McDialogHeader>
          <McDialogTitle>Create project</McDialogTitle>
        </McDialogHeader>

        <form
          id="create-project-form"
          onSubmit={(e) => {
            e.preventDefault()
            createProject.mutate()
          }}
          className="flex flex-col gap-3"
        >
          <McInput
            label="Name"
            description="e.g. Acme Shop — the slug is derived from this."
            required
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
          />

          {createProject.isError && (
            <p className="text-sm text-destructive">
              {createProject.error instanceof ApiError ? createProject.error.message : 'could not create project'}
            </p>
          )}
        </form>

        <McDialogFooter>
          <McButton variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </McButton>
          <McButton type="submit" form="create-project-form" isLoading={createProject.isPending}>
            Create
          </McButton>
        </McDialogFooter>
      </McDialogContent>
    </McDialog>
  )
}
