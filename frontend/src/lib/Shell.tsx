import { Link, useLocation } from '@tanstack/react-router'
import { FolderGit2, LogOut, Users as UsersIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useAuth } from './auth'
import {
  McSidebar,
  McSidebarContent,
  McSidebarFooter,
  McSidebarGroup,
  McSidebarGroupLabel,
  McSidebarHeader,
  McSidebarInset,
  McSidebarMenu,
  McSidebarMenuButton,
  McSidebarMenuItem,
  McSidebarProvider,
  McSidebarTrigger,
} from '../components/mc-sidebar'
import { McAvatar, McAvatarFallback } from '../components/mc-avatar'
import {
  McDropdownMenu,
  McDropdownMenuContent,
  McDropdownMenuItem,
  McDropdownMenuTrigger,
} from '../components/mc-dropdown-menu'
import { McSeparator } from '../components/mc-separator'

const NAV_ITEMS = [
  { to: '/' as const, label: 'Users', icon: UsersIcon },
  { to: '/projects' as const, label: 'Projects', icon: FolderGit2 },
]

function isActive(pathname: string, to: string) {
  return to === '/' ? pathname === '/' : pathname.startsWith(to)
}

export function Shell({ children }: { children: ReactNode }) {
  const { owner, logout } = useAuth()
  const location = useLocation()

  const initials = (owner?.name ?? owner?.email ?? '?').slice(0, 2).toUpperCase()

  return (
    <McSidebarProvider>
      <McSidebar>
        <McSidebarHeader>
          <span className="header-xs px-2 text-lg font-bold text-sidebar-foreground">m-Seismo</span>
        </McSidebarHeader>

        <McSidebarContent>
          <McSidebarGroup>
            <McSidebarGroupLabel>Workspace</McSidebarGroupLabel>
            <McSidebarMenu>
              {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
                <McSidebarMenuItem key={to}>
                  <McSidebarMenuButton
                    isActive={isActive(location.pathname, to)}
                    tooltip={label}
                    render={<Link to={to} />}
                  >
                    <Icon />
                    <span>{label}</span>
                  </McSidebarMenuButton>
                </McSidebarMenuItem>
              ))}
            </McSidebarMenu>
          </McSidebarGroup>
        </McSidebarContent>

        <McSidebarFooter>
          <McDropdownMenu>
            <McDropdownMenuTrigger
              render={
                <button className="flex w-full items-center gap-2 rounded-lg p-2 text-left hover:bg-sidebar-accent">
                  <McAvatar className="size-8">
                    <McAvatarFallback>{initials}</McAvatarFallback>
                  </McAvatar>
                  <span className="min-w-0 flex-1 truncate text-sm font-medium text-sidebar-foreground">
                    {owner?.name ?? owner?.email}
                  </span>
                </button>
              }
            />
            <McDropdownMenuContent align="start" side="top">
              <McDropdownMenuItem onClick={logout}>
                <LogOut />
                Sign out
              </McDropdownMenuItem>
            </McDropdownMenuContent>
          </McDropdownMenu>
        </McSidebarFooter>
      </McSidebar>

      <McSidebarInset>
        <header className="flex items-center gap-3 border-b border-border bg-background px-4 py-3">
          <McSidebarTrigger />
          <McSeparator orientation="vertical" className="h-5" />
          <span className="text-sm font-medium text-foreground">
            {NAV_ITEMS.find((item) => isActive(location.pathname, item.to))?.label ?? 'm-Seismo'}
          </span>
        </header>

        <main className="mx-auto w-full max-w-5xl flex-1 space-y-8 p-6">{children}</main>
      </McSidebarInset>
    </McSidebarProvider>
  )
}
