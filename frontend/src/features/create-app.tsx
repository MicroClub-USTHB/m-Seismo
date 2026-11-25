import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Platform, type ProjectApp } from '../lib/api'
import { McButton } from './../components/mc-button'
import { McInput } from './../components/mc-input'
import { McDialog, McDialogContent, McDialogFooter, McDialogHeader, McDialogTitle, McDialogTrigger } from '../components/mc-dialog'
import { McSelect, McSelectContent, McSelectItem, McSelectTrigger, McSelectValue } from '../components/mc-select'

export const PLATFORMS: Platform[] = ['web', 'ios', 'android']
export const PLATFORM_LABEL: Record<Platform, string> = { web: 'Web', ios: 'iOS', android: 'Android' }

export function CreateAppForm({ projectId, onCreated }: { projectId: string; onCreated?: (app: ProjectApp) => void }) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [platform, setPlatform] = useState<Platform>('web')
  const [bundleId, setBundleId] = useState('')

  const createApp = useMutation({
    mutationFn: () => api.post<ProjectApp>(`/projects/${projectId}/apps`, { name, platform, bundle_id: bundleId }),
    onSuccess: (app) => {
      queryClient.invalidateQueries({ queryKey: ['projects', projectId, 'apps'] })
      setName('')
      setBundleId('')
      setOpen(false)
      onCreated?.(app)
    },
  })

  return (
    <McDialog open={open} onOpenChange={setOpen}>
      <McDialogTrigger render={<McButton>Create app</McButton>} />
      <McDialogContent>
        <McDialogHeader>
          <McDialogTitle>Create app</McDialogTitle>
        </McDialogHeader>
        <form
          id="create-project-app-form"
          onSubmit={(e) => {
            e.preventDefault()
            createApp.mutate()
          }}
          className="flex flex-col gap-3"
        >
          <McInput label="Name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          <div className="flex flex-col gap-1.5">
            <label className="text-sm font-medium text-muted-foreground">Platform</label>
            <McSelect value={platform} onValueChange={(value) => setPlatform(value as Platform)}>
              <McSelectTrigger className="w-full">
                <McSelectValue />
              </McSelectTrigger>
              <McSelectContent>
                {PLATFORMS.map((p) => (
                  <McSelectItem key={p} value={p}>
                    {PLATFORM_LABEL[p]}
                  </McSelectItem>
                ))}
              </McSelectContent>
            </McSelect>
          </div>
          <McInput label="Bundle ID" description="e.g. com.example.shop" required value={bundleId} onChange={(e) => setBundleId(e.target.value)} />
          {createApp.isError && (
            <p className="text-sm text-destructive">
              {createApp.error instanceof ApiError ? createApp.error.message : 'could not create app'}
            </p>
          )}
        </form>
        <McDialogFooter>
          <McButton variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </McButton>
          <McButton type="submit" form="create-project-app-form" isLoading={createApp.isPending}>
            Create
          </McButton>
        </McDialogFooter>
      </McDialogContent>
    </McDialog>
  )
}
