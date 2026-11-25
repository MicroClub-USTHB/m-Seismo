import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, CATEGORIES, SEVERITIES, type ProjectApp, type RepositoryLink, type RoutingRule } from '../../lib/api'
import { McButton } from '../../components/mc-button'
import { McInput } from '../../components/mc-input'
import { McBadge } from '../../components/mc-badge'

const errorText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)
const csv = (s: string) => s.split(',').map((x) => x.trim()).filter(Boolean)
const selectCls = 'rounded-md border border-border bg-background px-2 py-1 text-sm'

export function RoutingStep({ projectId, apps }: { projectId: string; apps: ProjectApp[] }) {
  const queryClient = useQueryClient()
  const rulesKey = ['projects', projectId, 'routing-rules']
  const rules = useQuery({ queryKey: rulesKey, queryFn: () => api.get<RoutingRule[]>(`/projects/${projectId}/routing-rules`) })
  const repos = useQuery({
    queryKey: ['projects', projectId, 'repositories'],
    queryFn: () => api.get<RepositoryLink[]>(`/projects/${projectId}/repositories`),
  })
  const refresh = () => queryClient.invalidateQueries({ queryKey: rulesKey })

  const [appId, setAppId] = useState('')
  const [severity, setSeverity] = useState('')
  const [category, setCategory] = useState('')
  const [repositoryId, setRepositoryId] = useState('')
  const [labels, setLabels] = useState('')
  const [assignees, setAssignees] = useState('')
  const [milestone, setMilestone] = useState('')

  const repoName = (id: string) => repos.data?.find((r) => r.id === id)?.repository ?? id
  const appName = (id: string | null) => (id ? (apps.find((a) => a.id === id)?.name ?? id) : 'any app')

  const create = useMutation({
    mutationFn: () =>
      api.post(`/projects/${projectId}/routing-rules`, {
        app_id: appId || null,
        severity: severity || null,
        category: category || null,
        repository_id: repositoryId || repos.data?.[0]?.id,
        labels: csv(labels),
        assignees: csv(assignees),
        milestone: milestone || null,
      }),
    onSuccess: () => {
      setLabels('')
      setAssignees('')
      setMilestone('')
      refresh()
    },
  })
  const remove = useMutation({ mutationFn: (id: string) => api.delete(`/projects/${projectId}/routing-rules/${id}`), onSuccess: refresh })
  // Positions are absolute and may repeat, so a move renumbers every rule to
  // match the new order instead of nudging one of them.
  const move = useMutation({
    mutationFn: async ({ from, to }: { from: number; to: number }) => {
      const order = [...(rules.data ?? [])]
      const [moved] = order.splice(from, 1)
      order.splice(to, 0, moved)
      for (const [position, rule] of order.entries()) {
        if (rule.position === position) continue
        await api.put(`/projects/${projectId}/routing-rules/${rule.id}`, {
          position,
          app_id: rule.app_id,
          severity: rule.severity,
          category: rule.category,
          repository_id: rule.repository_id,
          labels: rule.labels,
          assignees: rule.assignees,
          milestone: rule.milestone,
        })
      }
    },
    onSettled: refresh,
  })

  const noRepos = repos.data?.length === 0
  return (
    <section className="space-y-6" data-testid="step-routing">
      <p className="text-sm text-muted-foreground">
        Categorize reports: the first rule that matches picks the repository, labels, assignees and milestone. A report no rule matches goes to the default repository. Leave a field on "any" to match everything.
      </p>

      <ol className="space-y-2" data-testid="rule-list">
        {rules.data?.length === 0 && <li className="text-sm text-muted-foreground">No rules yet. Every report goes to the default repository.</li>}
        {rules.data?.map((r, i) => (
          <li key={r.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border p-3 text-sm">
            <span className="text-muted-foreground">{i + 1}.</span>
            <McBadge>{appName(r.app_id)}</McBadge>
            <McBadge>{r.severity ?? 'any severity'}</McBadge>
            <McBadge>{r.category ?? 'any category'}</McBadge>
            <span>→ {repoName(r.repository_id)}</span>
            {r.labels.map((l) => (
              <McBadge key={l}>{l}</McBadge>
            ))}
            {r.assignees.length > 0 && <span className="text-muted-foreground">@{r.assignees.join(', @')}</span>}
            {r.milestone && <span className="text-muted-foreground">milestone {r.milestone}</span>}
            <span className="ml-auto flex gap-1">
              <McButton variant="tertiary" size="sm" disabled={i === 0} onClick={() => move.mutate({ from: i, to: i - 1 })}>
                Up
              </McButton>
              <McButton
                variant="tertiary"
                size="sm"
                disabled={i === (rules.data?.length ?? 0) - 1}
                onClick={() => move.mutate({ from: i, to: i + 1 })}
              >
                Down
              </McButton>
              <McButton variant="tertiary" size="sm" onClick={() => remove.mutate(r.id)}>
                Delete
              </McButton>
            </span>
          </li>
        ))}
      </ol>
      {(remove.isError || move.isError) && <p className="text-sm text-destructive">{errorText(remove.error ?? move.error, 'request failed')}</p>}

      <form
        className="space-y-3 rounded-md border border-border p-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <h4 className="text-sm font-medium text-foreground">Add a rule</h4>
        {noRepos && <p className="text-sm text-destructive">Link a repository first (previous step).</p>}
        <div className="flex flex-wrap gap-3 text-sm">
          <label className="flex flex-col gap-1">
            App
            <select aria-label="App" className={selectCls} value={appId} onChange={(e) => setAppId(e.target.value)}>
              <option value="">any app</option>
              {apps.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Severity
            <select aria-label="Severity" className={selectCls} value={severity} onChange={(e) => setSeverity(e.target.value)}>
              <option value="">any severity</option>
              {SEVERITIES.map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Category
            <select aria-label="Category" className={selectCls} value={category} onChange={(e) => setCategory(e.target.value)}>
              <option value="">any category</option>
              {CATEGORIES.map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Repository
            <select aria-label="Repository" className={selectCls} value={repositoryId || repos.data?.[0]?.id || ''} onChange={(e) => setRepositoryId(e.target.value)}>
              {repos.data?.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.repository}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="grid gap-3 sm:grid-cols-3">
          <McInput label="Labels" description="comma separated" value={labels} onChange={(e) => setLabels(e.target.value)} />
          <McInput label="Assignees" description="comma separated" value={assignees} onChange={(e) => setAssignees(e.target.value)} />
          <McInput label="Milestone" value={milestone} onChange={(e) => setMilestone(e.target.value)} />
        </div>
        <McButton type="submit" disabled={noRepos} isLoading={create.isPending}>
          Add rule
        </McButton>
        {create.isError && <p className="text-sm text-destructive">{errorText(create.error, 'could not add the rule')}</p>}
      </form>
    </section>
  )
}
