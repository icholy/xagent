import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getGitHubAccount,
  unlinkGitHubAccount,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ExternalLink, Github, Loader2, Unlink } from 'lucide-react'

export const Route = createFileRoute('/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  const { data, isLoading, refetch } = useQuery(getGitHubAccount, {})
  const unlinkMutation = useMutation(unlinkGitHubAccount, {
    onSuccess: () => refetch(),
  })

  const account = data?.account
  const appSlug = data?.githubAppSlug

  return (
    <div className="container mx-auto py-8 px-4">
      <h1 className="text-2xl font-bold mb-6">Settings</h1>
      <Card>
        <CardHeader>
          <CardTitle>GitHub Account</CardTitle>
          <CardDescription>
            Link your GitHub account to receive webhook notifications for your tasks.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="text-muted-foreground">Loading...</div>
          ) : account ? (
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <Github className="h-5 w-5" />
                <span className="font-medium">{account.githubUsername}</span>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => unlinkMutation.mutateAsync({})}
                disabled={unlinkMutation.isPending}
              >
                {unlinkMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Unlink className="h-4 w-4" />
                )}
                Unlink
              </Button>
            </div>
          ) : (
            <a href="/github/login">
              <Button>
                <Github className="h-4 w-4" />
                Link GitHub Account
              </Button>
            </a>
          )}
          {appSlug && (
            <a
              href={`https://github.com/apps/${appSlug}/installations/new`}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button variant="outline">
                <ExternalLink className="h-4 w-4" />
                Install GitHub App
              </Button>
            </a>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
