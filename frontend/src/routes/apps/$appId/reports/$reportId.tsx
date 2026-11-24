import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api, type ReportDetail } from '../../../../lib/api'
import { getStoredOwner } from '../../../../lib/auth'
import { Shell } from '../../../../lib/Shell'
import { McCard } from '../../../../components/mc-card'
import { decodePayload, formatDate, RetryButton, StatusBadge } from '../../../../features/reports'

export const Route = createFileRoute('/apps/$appId/reports/$reportId')({
  beforeLoad: () => {
    if (!getStoredOwner()) throw redirect({ to: '/login' })
  },
  component: ReportPage,
})

function ReportPage() {
  const { appId, reportId } = Route.useParams()

  const report = useQuery({
    queryKey: ['apps', appId, 'reports', 'detail', reportId],
    queryFn: () => api.get<ReportDetail>(`/apps/${appId}/reports/${reportId}`),
  })
  const r = report.data

  return (
    <Shell>
      <div className="space-y-1">
        <Link
          to="/apps/$appId/reports"
          params={{ appId }}
          className="text-sm text-muted-foreground hover:text-foreground hover:underline"
        >
          ← Reports
        </Link>
        <h2 className="header-xs break-all text-foreground">Report {reportId}</h2>
      </div>

      {report.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {report.isError && <p className="text-sm text-destructive">{report.error.message}</p>}

      {r && (
        <>
          <McCard className="gap-0 p-0">
            <dl className="divide-y divide-border text-sm">
              <Row label="Status">
                <span className="inline-flex items-center gap-3">
                  <StatusBadge status={r.status} />
                  {r.status === 'failed' && <RetryButton appId={appId} reportId={r.report_id} size="sm" />}
                </span>
              </Row>
              <Row label="Trigger">{r.trigger}</Row>
              <Row label="Occurred">{formatDate(r.occurred_at)}</Row>
              <Row label="Received">{formatDate(r.received_at)}</Row>
              <Row label="Processed">{formatDate(r.processed_at)}</Row>
              <Row label="Installation">
                <code className="break-all">{r.installation_id}</code>
              </Row>
              <Row label="Session">
                <code className="break-all">{r.session_id}</code>
              </Row>
            </dl>
          </McCard>

          <section className="space-y-3">
            <h3 className="text-sm font-medium text-foreground">Issues</h3>
            {r.deliveries.length === 0 ? (
              <p className="text-sm text-muted-foreground">No issue opened for this report yet.</p>
            ) : (
              <McCard className="divide-y divide-border p-0">
                {r.deliveries.map((d) => (
                  <div key={d.url} className="flex items-center justify-between gap-4 px-4 py-2.5 text-sm">
                    <a href={d.url} target="_blank" rel="noreferrer" className="break-all hover:underline">
                      {d.url}
                    </a>
                    <span className="whitespace-nowrap text-muted-foreground">{formatDate(d.created_at)}</span>
                  </div>
                ))}
              </McCard>
            )}
          </section>

          <section className="space-y-3">
            <h3 className="text-sm font-medium text-foreground">Payload</h3>
            <McCard className="p-0">
              <pre className="max-h-[32rem] overflow-auto p-4 text-xs">
                {JSON.stringify(decodePayload(r.payload), null, 2)}
              </pre>
            </McCard>
          </section>
        </>
      )}
    </Shell>
  )
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[8rem_1fr] gap-4 px-4 py-2.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-foreground">{children}</dd>
    </div>
  )
}
