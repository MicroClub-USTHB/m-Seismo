import { createFileRoute, redirect } from '@tanstack/react-router'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Owner } from '../lib/api'
import { getStoredOwner } from '../lib/auth'
import { Shell } from '../lib/Shell'
import { McButton } from '../components/mc-button'
import { McInput } from '../components/mc-input'
import { McCard, McCardHeader } from '../components/mc-card'
import { McDataTable, type McDataTableColumnDef } from '../components/mc-data-table'
import {
  McDialog,
  McDialogContent,
  McDialogFooter,
  McDialogHeader,
  McDialogTitle,
  McDialogTrigger,
} from '../components/mc-dialog'

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: Dashboard,
})

const columns: McDataTableColumnDef<Owner>[] = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'email', header: 'Email' },
  {
    accessorKey: 'created_at',
    header: 'Created',
    cell: (ctx) => new Date(ctx.getValue() as string).toLocaleString(),
  },
]

function Dashboard() {
  const owners = useQuery({
    queryKey: ['owners'],
    queryFn: () => api.get<Owner[]>('/owners'),
  })

  return (
    <Shell>
      <McCard direction="row" className="items-center justify-between">
        <McCardHeader title="Users" description="Owners with access to this m-Seismo workspace." />
        <AddUserForm />
      </McCard>

      {owners.isError && (
        <p className="text-sm text-destructive">
          {owners.error instanceof ApiError ? owners.error.message : 'could not load users'}
        </p>
      )}

      <McDataTable
        columns={columns}
        data={owners.data ?? []}
        getRowId={(owner) => owner.id}
        empty={<p className="p-6 text-center text-sm text-muted-foreground">No users yet.</p>}
      />
    </Shell>
  )
}

function AddUserForm() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')

  const createUser = useMutation({
    mutationFn: () => api.post<Owner>('/auth/register', { email, name, password }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['owners'] })
      setEmail('')
      setName('')
      setPassword('')
      setOpen(false)
    },
  })

  return (
    <McDialog open={open} onOpenChange={setOpen}>
      <McDialogTrigger render={<McButton>Add user</McButton>} />
      <McDialogContent>
        <McDialogHeader>
          <McDialogTitle>Add user</McDialogTitle>
        </McDialogHeader>

        <form
          id="add-user-form"
          onSubmit={(e) => {
            e.preventDefault()
            createUser.mutate()
          }}
          className="flex flex-col gap-3"
        >
          <McInput label="Name" required value={name} onChange={(e) => setName(e.target.value)} />
          <McInput
            label="Email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <McInput
            label="Password"
            type="password"
            required
            minLength={8}
            description="Minimum 8 characters."
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />

          {createUser.isError && (
            <p className="text-sm text-destructive">
              {createUser.error instanceof ApiError ? createUser.error.message : 'could not create user'}
            </p>
          )}
        </form>

        <McDialogFooter>
          <McButton variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </McButton>
          <McButton type="submit" form="add-user-form" isLoading={createUser.isPending}>
            Create
          </McButton>
        </McDialogFooter>
      </McDialogContent>
    </McDialog>
  )
}
