import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type ReportStatus } from '../lib/api'
import { McBadge } from '../components/mc-badge'
import { McButton } from '../components/mc-button'

export function StatusBadge({ status }: { status: ReportStatus }) {
  return status === 'delivered' ? (
    <McBadge variant="outline" className="text-success">
      Delivered
    </McBadge>
  ) : (
    <McBadge variant="destructive">Not delivered</McBadge>
  )
}

export function formatDate(iso: string | undefined) {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

/** Re-queues a report so the worker tries to deliver it again. */
export function RetryButton({ appId, reportId, size }: { appId: string; reportId: string; size?: 'sm' }) {
  const queryClient = useQueryClient()
  const retry = useMutation({
    mutationFn: () => api.post(`/apps/${appId}/reports/${reportId}/retry`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['apps', appId, 'reports'] }),
  })

  return (
    <span className="inline-flex items-center gap-2">
      <McButton variant="secondary" size={size} onClick={() => retry.mutate()} isLoading={retry.isPending}>
        Retry
      </McButton>
      {retry.isSuccess && <span className="text-xs text-muted-foreground">Queued</span>}
      {retry.isError && <span className="text-xs text-destructive">{retry.error.message}</span>}
    </span>
  )
}

/** The payload comes back as base64 of the stored JSON; show it readable. */
export function decodePayload(payload: unknown): unknown {
  if (typeof payload !== 'string') return payload
  try {
    const bytes = Uint8Array.from(atob(payload), (c) => c.charCodeAt(0))
    return JSON.parse(new TextDecoder().decode(bytes))
  } catch {
    return payload
  }
}
