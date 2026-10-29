import { useState } from 'react'
import type { ProjectApp } from '../../lib/api'
import { KeysSection, TestIssueSection } from '../app-sections'

export function DoneStep({ apps }: { apps: ProjectApp[] }) {
  const [appId, setAppId] = useState(apps[0]?.id ?? '')
  return (
    <section className="space-y-6" data-testid="step-done">
      <p className="text-sm text-muted-foreground">
        Create the SDK key your app ships with, then send a test issue to check the whole path: key, tracker, repository, rules.
      </p>
      <div className="flex items-center gap-2">
        <label htmlFor="done-app" className="text-sm font-medium text-muted-foreground">
          App
        </label>
        <select id="done-app" value={appId} onChange={(e) => setAppId(e.target.value)} className="rounded-md border border-border bg-background px-2 py-1 text-sm">
          {apps.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </select>
      </div>
      {appId && <KeysSection key={`k-${appId}`} appId={appId} />}
      {appId && <TestIssueSection key={`t-${appId}`} appId={appId} />}
    </section>
  )
}
