import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { getProfile } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { authTransport } from '@/lib/transport'
import { useOrgId } from '@/hooks/use-org-id'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export const Route = createFileRoute('/oauth/authorize')({
  component: OAuthAuthorizePage,
})

function OAuthAuthorizePage() {
  const params = new URLSearchParams(window.location.search)
  const clientId = params.get('client_id') ?? ''
  const redirectUri = params.get('redirect_uri') ?? ''
  const state = params.get('state') ?? ''
  const codeChallenge = params.get('code_challenge') ?? ''
  const codeChallengeMethod = params.get('code_challenge_method') ?? ''
  const responseType = params.get('response_type') ?? ''

  const { data: profileData, isLoading } = useQuery(getProfile, {})
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const profile = profileData?.profile
  const orgs = profileData?.orgs ?? []
  const currentOrgId = useOrgId()
  const currentOrg = orgs.find((o) => String(o.id) === currentOrgId)

  const handleApprove = async () => {
    setError(null)
    setSubmitting(true)
    try {
      const token = await authTransport.fetchToken()
      const body = new URLSearchParams({
        token: token,
        client_id: clientId,
        redirect_uri: redirectUri,
        state,
        code_challenge: codeChallenge,
        code_challenge_method: codeChallengeMethod,
        response_type: responseType,
      })
      const resp = await fetch('/oauth/authorize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body.toString(),
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(text || `Authorization failed (${resp.status})`)
      }
      const data = await resp.json()
      window.location.href = data.redirect_uri
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unknown error')
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

  if (!clientId || !redirectUri || !codeChallenge) {
    return (
      <div className="container mx-auto py-8 px-4">
        <Card>
          <CardContent className="pt-6">
            <p className="text-destructive">Missing required OAuth parameters.</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="container mx-auto py-8 px-4 max-w-md">
      <h1 className="text-2xl font-bold mb-6">Authorize Application</h1>

      <Card>
        <CardContent className="pt-6 space-y-4">
          <p className="text-sm text-muted-foreground">
            An application is requesting access to your account.
          </p>

          <div className="space-y-2">
            <div className="text-sm">
              <span className="text-muted-foreground">Signed in as: </span>
              <span className="font-medium">{profile?.name || profile?.email || 'Unknown'}</span>
            </div>
            {profile?.email && profile?.name && (
              <div className="text-sm">
                <span className="text-muted-foreground">Email: </span>
                <span>{profile.email}</span>
              </div>
            )}
            {currentOrg && (
              <div className="text-sm">
                <span className="text-muted-foreground">Organization: </span>
                <span className="font-medium">{currentOrg.name}</span>
              </div>
            )}
          </div>

          {error && (
            <div className="text-destructive text-sm">{error}</div>
          )}

          <div className="flex gap-2 pt-2">
            <Button onClick={handleApprove} disabled={submitting}>
              {submitting ? 'Authorizing...' : 'Approve'}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                const redirectUrl = new URL(redirectUri)
                redirectUrl.searchParams.set('error', 'access_denied')
                if (state) {
                  redirectUrl.searchParams.set('state', state)
                }
                window.location.href = redirectUrl.toString()
              }}
              disabled={submitting}
            >
              Deny
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
