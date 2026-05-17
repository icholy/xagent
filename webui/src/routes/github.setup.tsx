import { useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { ConnectError, Code } from '@connectrpc/connect'
import { getProfile, linkGitHubInstallation } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { useAuthTransport } from '@/lib/services'
import { useOrgId } from '@/hooks/use-org-id'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export const Route = createFileRoute('/github/setup')({
  component: GitHubSetupPage,
})

function GitHubSetupPage() {
  const installationIdParam = new URLSearchParams(window.location.search).get('installation_id')
  const installationId = installationIdParam ? BigInt(installationIdParam) : null

  const { data: profileData, isLoading } = useQuery(getProfile, {})
  const profile = profileData?.profile
  const githubAccount = profileData?.githubAccount
  const orgs = profileData?.orgs ?? []
  const currentOrgId = useOrgId()
  const auth = useAuthTransport()

  const [selectedOrgId, setSelectedOrgId] = useState<string>('')
  const [error, setError] = useState<string | null>(null)
  const [needsGitHubLink, setNeedsGitHubLink] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!selectedOrgId && orgs.length > 0) {
      const fallback = orgs.find((o) => String(o.id) === currentOrgId) ?? orgs[0]
      setSelectedOrgId(String(fallback.id))
    }
  }, [orgs, currentOrgId, selectedOrgId])

  const mutation = useMutation(linkGitHubInstallation)

  const handleLink = async () => {
    if (!installationId || !selectedOrgId) return
    setError(null)
    setNeedsGitHubLink(false)
    setSubmitting(true)
    try {
      await auth.fetchToken(selectedOrgId)
      await mutation.mutateAsync({ installationId })
      window.location.href = '/ui/settings?tab=organisation'
    } catch (e) {
      if (e instanceof ConnectError && e.code === Code.FailedPrecondition) {
        setNeedsGitHubLink(true)
      } else {
        setError(e instanceof Error ? e.message : 'Unknown error')
      }
      setSubmitting(false)
    }
  }

  if (isLoading) {
    return (
      <div className="container mx-auto py-8 px-4">
        <p className="text-muted-foreground">Loading...</p>
      </div>
    )
  }

  if (!installationId) {
    return (
      <div className="container mx-auto py-8 px-4 max-w-md">
        <Card>
          <CardContent className="pt-6">
            <p className="text-destructive">Missing installation_id query parameter.</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  const hasGitHubAccount = !!githubAccount
  const showGitHubLinkPrompt = needsGitHubLink || !hasGitHubAccount

  return (
    <div className="container mx-auto py-8 px-4 max-w-md">
      <h1 className="text-2xl font-bold mb-6">Link GitHub App Installation</h1>

      <Card>
        <CardHeader>
          <CardTitle>Choose organisation</CardTitle>
          <CardDescription>
            Select the organisation to associate with this GitHub App installation.
            Only the GitHub user who installed the App can complete this link.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2 text-sm">
            <div>
              <span className="text-muted-foreground">Signed in as: </span>
              <span className="font-medium">{profile?.name || profile?.email || 'Unknown'}</span>
            </div>
            {githubAccount && (
              <div>
                <span className="text-muted-foreground">GitHub account: </span>
                <span className="font-medium">{githubAccount.githubUsername}</span>
              </div>
            )}
            <div>
              <span className="text-muted-foreground">Installation ID: </span>
              <span className="font-mono">{String(installationId)}</span>
            </div>
          </div>

          {showGitHubLinkPrompt && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm space-y-2">
              <p className="text-destructive">
                You need to link your GitHub account before completing this step.
              </p>
              <a href="/github/login" className="inline-block">
                <Button variant="outline" size="sm">Link GitHub Account</Button>
              </a>
            </div>
          )}

          <Select value={selectedOrgId} onValueChange={setSelectedOrgId} disabled={showGitHubLinkPrompt}>
            <SelectTrigger>
              <SelectValue placeholder="Select organisation" />
            </SelectTrigger>
            <SelectContent>
              {orgs.map((org) => (
                <SelectItem key={String(org.id)} value={String(org.id)}>
                  {org.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {error && <div className="text-destructive text-sm">{error}</div>}

          <Button onClick={handleLink} disabled={submitting || !selectedOrgId || showGitHubLinkPrompt}>
            {submitting ? 'Linking...' : 'Link installation'}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
