import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { api, ApiError, type App, type Platform } from '../../lib/api'
import { getStoredOwner } from '../../lib/auth'
import { Shell } from '../../lib/Shell'

export const Route = createFileRoute('/apps/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: AppsPage,
})

const columns: ColumnDef<App>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: (ctx) => (
      <Link to="/apps/$appId" params={{ appId: ctx.row.original.id }} className="text-gray-900 underline hover:no-underline">
        {ctx.getValue() as string}
      </Link>
    ),
  },
  { accessorKey: 'platform', header: 'Platform' },
  { accessorKey: 'bundle_id', header: 'Bundle ID' },
  {
    accessorKey: 'created_at',
    header: 'Created',
    cell: (ctx) => new Date(ctx.getValue() as string).toLocaleString(),
  },
]

const PLATFORMS: Platform[] = ['ios', 'android', 'flutter', 'react_native']

function AppsPage() {
  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<App[]>('/apps'),
  })

  const table = useReactTable({
    data: apps.data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  return (
    <Shell>
      <CreateAppForm />

      <section>
        <h2 className="mb-3 text-sm font-medium text-gray-700">Apps</h2>

        {apps.isLoading && <p className="text-sm text-gray-500">Loading…</p>}
        {apps.isError && (
          <p className="text-sm text-red-600">
            {apps.error instanceof ApiError ? apps.error.message : 'could not load apps'}
          </p>
        )}

        {apps.data && (
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
              {table.getRowModel().rows.length === 0 && (
                <tr>
                  <td colSpan={columns.length} className="px-4 py-6 text-center text-gray-500">
                    No apps yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </section>
    </Shell>
  )
}

function CreateAppForm() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [platform, setPlatform] = useState<Platform>('ios')
  const [bundleId, setBundleId] = useState('')

  const createApp = useMutation({
    mutationFn: () => api.post<App>('/apps', { name, platform, bundle_id: bundleId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] })
      setName('')
      setBundleId('')
      setOpen(false)
    },
  })

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800"
      >
        Create app
      </button>
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        createApp.mutate()
      }}
      className="max-w-sm space-y-3 rounded-lg border bg-white p-4"
    >
      <h2 className="text-sm font-medium text-gray-700">Create app</h2>

      <input
        type="text"
        placeholder="Name"
        required
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      />
      <select
        value={platform}
        onChange={(e) => setPlatform(e.target.value as Platform)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      >
        {PLATFORMS.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
      <input
        type="text"
        placeholder="Bundle ID (e.g. com.example.shop)"
        required
        value={bundleId}
        onChange={(e) => setBundleId(e.target.value)}
        className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
      />

      {createApp.isError && (
        <p className="text-sm text-red-600">
          {createApp.error instanceof ApiError ? createApp.error.message : 'could not create app'}
        </p>
      )}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={createApp.isPending}
          className="rounded bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
        >
          {createApp.isPending ? 'Creating…' : 'Create'}
        </button>
        <button type="button" onClick={() => setOpen(false)} className="px-3 py-2 text-sm text-gray-600">
          Cancel
        </button>
      </div>
    </form>
  )
}
