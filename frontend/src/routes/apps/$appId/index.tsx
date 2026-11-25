import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/apps/$appId/')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/apps/$appId/connections', params })
  },
})
