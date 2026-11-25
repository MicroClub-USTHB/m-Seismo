import { Link } from '@tanstack/react-router'

const TABS = [
  { to: '/apps/$appId/connections' as const, label: 'Settings' },
  { to: '/apps/$appId/reports' as const, label: 'Reports' },
]

/** Switches between the pages of one app. */
export function AppTabs({ appId }: { appId: string }) {
  return (
    <nav className="flex gap-1 border-b border-border">
      {TABS.map((tab) => (
        <Link
          key={tab.to}
          to={tab.to}
          params={{ appId }}
          className="-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground hover:text-foreground"
          activeProps={{ className: 'border-primary text-foreground font-medium' }}
          activeOptions={{ exact: false, includeSearch: false }}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  )
}
