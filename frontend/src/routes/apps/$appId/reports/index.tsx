import { createFileRoute, Link, redirect, useNavigate } from '@tanstack/react-router'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { api, type App, type ReportPage, type ReportStatus } from '../../../../lib/api'
import { getStoredOwner } from '../../../../lib/auth'
import { Shell } from '../../../../lib/Shell'
import { McButton } from '../../../../components/mc-button'
import { McCard } from '../../../../components/mc-card'
import { McInput } from '../../../../components/mc-input'
import {
  McSelect,
  McSelectContent,
  McSelectItem,
  McSelectTrigger,
  McSelectValue,
} from '../../../../components/mc-select'
import { AppTabs } from '../../../../features/app-tabs'
import { formatDate, RetryButton, StatusBadge } from '../../../../features/reports'

interface ReportsSearch {
  status?: ReportStatus
  /** yyyy-mm-dd: show reports that happened on or before this day. */
  before?: string
}

const PAGE_SIZE = 25

export const Route = createFileRoute('/apps/$appId/reports/')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  validateSearch: (search: Record<string, unknown>): ReportsSearch => ({
    status:
      search.status === 'delivered' || search.status === 'queued' || search.status === 'failed'
        ? search.status
        : undefined,
    before: typeof search.before === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(search.before) ? search.before : undefined,
  }),
  component: ReportsPage,
})

/** The list is newest first and the cursor is exclusive, so "on or before
 * day D" starts at the first instant of D+1 in the viewer's time zone. */
function dayEndCursor(day: string) {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(y, m - 1, d + 1).toISOString()
}

function ReportsPage() {
  const { appId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })

  const app = useQuery({
    queryKey: ['apps', appId],
    queryFn: () => api.get<App>(`/apps/${appId}`),
  })
  const apps = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.get<App[]>('/apps'),
  })

  const reports = useInfiniteQuery({
    queryKey: ['apps', appId, 'reports', search.status ?? 'all', search.before ?? ''],
    initialPageParam: search.before ? dayEndCursor(search.before) : '',
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(PAGE_SIZE) })
      if (search.status) q.set('status', search.status)
      if (pageParam) q.set('cursor', pageParam)
      return api.get<ReportPage>(`/apps/${appId}/reports?${q}`)
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  const rows = reports.data?.pages.flatMap((p) => p.reports) ?? []

  return (
    <Shell>
      <div className="space-y-1">
        <Link to="/projects" className="text-sm text-muted-foreground hover:text-foreground hover:underline">
          ← Projects
        </Link>
        <h2 className="header-xs text-foreground">{app.data?.name ?? '…'}</h2>
      </div>

      <AppTabs appId={appId} />

      <div className="flex flex-wrap items-end gap-3">
        {apps.data && apps.data.length > 1 && (
          <McSelect
            value={appId}
            onValueChange={(value) =>
              value && navigate({ to: '/apps/$appId/reports', params: { appId: value }, search })
            }
          >
            <McSelectTrigger className="w-56">
              <McSelectValue placeholder="App" />
            </McSelectTrigger>
            <McSelectContent>
              {apps.data.map((a) => (
                <McSelectItem key={a.id} value={a.id}>
                  {a.name}
                </McSelectItem>
              ))}
            </McSelectContent>
          </McSelect>
        )}

        <McSelect
          value={search.status ?? 'all'}
          onValueChange={(value) =>
            navigate({
              search: (s: ReportsSearch) => ({ ...s, status: value === 'all' ? undefined : (value as ReportStatus) }),
            })
          }
        >
          <McSelectTrigger className="w-44">
            <McSelectValue />
          </McSelectTrigger>
          <McSelectContent>
            <McSelectItem value="all">All reports</McSelectItem>
            <McSelectItem value="delivered">Delivered</McSelectItem>
            <McSelectItem value="queued">Queued</McSelectItem>
            <McSelectItem value="failed">Failed</McSelectItem>
          </McSelectContent>
        </McSelect>

        <McInput
          type="date"
          aria-label="On or before"
          className="w-44"
          value={search.before ?? ''}
          onChange={(e) =>
            navigate({ search: (s: ReportsSearch) => ({ ...s, before: e.target.value || undefined }) })
          }
        />
      </div>

      {reports.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {reports.isError && <p className="text-sm text-destructive">{reports.error.message}</p>}
      {reports.isSuccess && rows.length === 0 && (
        <p className="text-sm text-muted-foreground">No reports match these filters.</p>
      )}

      {rows.length > 0 && (
        <McCard className="overflow-x-auto p-0">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-2.5 font-medium">Occurred</th>
                <th className="px-4 py-2.5 font-medium">Trigger</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Issue</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {rows.map((r) => (
                <tr key={r.report_id}>
                  <td className="whitespace-nowrap px-4 py-2.5">
                    <Link
                      to="/apps/$appId/reports/$reportId"
                      params={{ appId, reportId: r.report_id }}
                      className="text-foreground hover:underline"
                    >
                      {formatDate(r.occurred_at)}
                    </Link>
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground">{r.trigger}</td>
                  <td className="px-4 py-2.5">
                    <StatusBadge status={r.status} />
                    {r.delivery_error && (
                      <p className="mt-1 max-w-xs truncate text-xs text-destructive" title={r.delivery_error}>
                        {r.delivery_error}
                      </p>
                    )}
                  </td>
                  <td className="px-4 py-2.5">
                    {r.issue_urls.length === 0 && <span className="text-muted-foreground">—</span>}
                    {r.issue_urls.map((url) => (
                      <a key={url} href={url} target="_blank" rel="noreferrer" className="block hover:underline">
                        {issueLabel(url)}
                      </a>
                    ))}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    {r.status === 'failed' && <RetryButton appId={appId} reportId={r.report_id} size="sm" />}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </McCard>
      )}

      {reports.hasNextPage && (
        <McButton variant="secondary" onClick={() => reports.fetchNextPage()} isLoading={reports.isFetchingNextPage}>
          Load more
        </McButton>
      )}
    </Shell>
  )
}

/** "owner/repo#12" from a tracker issue URL, or the URL itself. */
function issueLabel(url: string) {
  const m = url.match(/^https?:\/\/[^/]+\/(.+?)\/(?:-\/)?issues\/(\d+)/)
  return m ? `${m[1]}#${m[2]}` : url
}
