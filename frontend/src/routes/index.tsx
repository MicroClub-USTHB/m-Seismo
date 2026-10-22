import { createFileRoute, redirect } from '@tanstack/react-router'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { api, ApiError, type Owner } from '../lib/api'
import { getStoredOwner } from '../lib/auth'
import { Shell } from '../lib/Shell'

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: Dashboard,
})

const columns: ColumnDef<Owner>[] = [
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

  const table = useReactTable({
    data: owners.data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Shell>
      <AddUserForm />

      <section>
        <h2 className="mb-3 text-sm font-medium text-gray-700">Users</h2>

          {owners.isLoading && <p className="text-sm text-gray-500">Loading…</p>}
          {owners.isError && (
            <p className="text-sm text-red-600">
              {owners.error instanceof ApiError
                ? owners.error.message
                : 'could not load users'}
            </p>
          )}

          {owners.data && (
            <table className="w-full overflow-hidden rounded-lg border bg-white text-sm">
              <thead className="bg-gray-100 text-left text-gray-600">
                {table.getHeaderGroups().map((hg) => (
                  <tr key={hg.id}>
                    {hg.headers.map((h) => (
                      <th key={h.id} className="px-4 py-2 font-medium">
                        {flexRender(h.column.columnDef.header, h.getContext())}
                      </th>
                    ))}
                  </tr>
                ))}
              </thead>
              <tbody>
                {table.getRowModel().rows.map((row) => (
                  <tr key={row.id} className="border-t">
                    {row.getVisibleCells().map((cell) => (
                      <td key={cell.id} className="px-4 py-2 text-gray-800">
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
      </section>
    </Shell>
  )
}

function AddUserForm() {
  const queryClient = useQueryClient()
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [open, setOpen] = useState(false)

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

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800"
      >
        Add user
      </button>
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        createUser.mutate()
      }}
      className="max-w-sm space-y-3 rounded-lg border bg-white p-4"
    >
      <h2 className="text-sm font-medium text-gray-700">Add user</h2>

      <input
        type="text"
        placeholder="Name"
        required
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      />
      <input
        type="email"
        placeholder="Email"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      />
      <input
        type="password"
        placeholder="Password (min 8 characters)"
        required
        minLength={8}
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      />

      {createUser.isError && (
        <p className="text-sm text-red-600">
          {createUser.error instanceof ApiError ? createUser.error.message : 'could not create user'}
        </p>
      )}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={createUser.isPending}
          className="rounded bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
        >
          {createUser.isPending ? 'Creating…' : 'Create'}
        </button>
        <button type="button" onClick={() => setOpen(false)} className="px-3 py-2 text-sm text-gray-600">
          Cancel
        </button>
      </div>
    </form>
  )
}
