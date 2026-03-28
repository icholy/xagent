import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getGitHubAccount,
  getProfile,
  unlinkGitHubAccount,
  createOrg,
  deleteOrg,
  listOrgMembers,
  addOrgMember,
  removeOrgMember,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ExternalLink, Github, Loader2, Plus, Trash2, Unlink, UserPlus } from 'lucide-react'
import { useOrgId } from '@/lib/use-org'

export const Route = createFileRoute('/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  const orgId = useOrgId()
  const { data, isLoading, refetch } = useQuery(getGitHubAccount, {})
  const { data: profileData, refetch: refetchProfile } = useQuery(getProfile, {})
  const unlinkMutation = useMutation(unlinkGitHubAccount, {
    onSuccess: () => refetch(),
  })

  const account = data?.account
  const appSlug = data?.githubAppSlug
  const orgs = profileData?.orgs ?? []
  const currentOrg = orgs.find((o) => o.id === orgId)
  const isOwner = currentOrg?.ownerId === profileData?.profile?.id

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
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

      <CreateOrgCard onCreated={refetchProfile} />

      {currentOrg && isOwner && (
        <OrgMembersCard orgId={orgId} orgName={currentOrg.name} />
      )}

      <OrgListCard
        orgs={orgs}
        currentUserId={profileData?.profile?.id ?? ''}
        onDeleted={refetchProfile}
      />
    </div>
  )
}

function CreateOrgCard({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const mutation = useMutation(createOrg, { onSuccess: () => { setName(''); onCreated() } })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create Organisation</CardTitle>
        <CardDescription>
          Create a new organisation to share tasks, keys, and workspaces with your team.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="flex items-end gap-3"
          onSubmit={async (e) => {
            e.preventDefault()
            if (!name.trim()) return
            await mutation.mutateAsync({ name: name.trim() })
          }}
        >
          <div className="space-y-2 flex-1">
            <Label htmlFor="org-name">Name</Label>
            <Input
              id="org-name"
              placeholder="e.g. My Team"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
            Create
          </Button>
        </form>
        {mutation.error && (
          <p className="text-destructive text-sm mt-2">{mutation.error.message}</p>
        )}
      </CardContent>
    </Card>
  )
}

function OrgMembersCard({ orgId, orgName }: { orgId: bigint; orgName: string }) {
  const [email, setEmail] = useState('')
  const { data, refetch } = useQuery(listOrgMembers, { orgId })
  const addMutation = useMutation(addOrgMember, { onSuccess: () => { setEmail(''); refetch() } })
  const removeMutation = useMutation(removeOrgMember, { onSuccess: () => refetch() })

  const members = data?.members ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>Members - {orgName}</CardTitle>
        <CardDescription>
          Manage who has access to this organisation's resources.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form
          className="flex items-end gap-3"
          onSubmit={async (e) => {
            e.preventDefault()
            if (!email.trim()) return
            await addMutation.mutateAsync({ orgId, email: email.trim() })
          }}
        >
          <div className="space-y-2 flex-1">
            <Label htmlFor="member-email">Add member by email</Label>
            <Input
              id="member-email"
              type="email"
              placeholder="user@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>
          <Button type="submit" disabled={addMutation.isPending}>
            {addMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <UserPlus className="h-4 w-4" />}
            Add
          </Button>
        </form>
        {addMutation.error && (
          <p className="text-destructive text-sm">{addMutation.error.message}</p>
        )}
        {members.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Email</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((m) => (
                <TableRow key={m.userId}>
                  <TableCell>{m.name}</TableCell>
                  <TableCell className="text-muted-foreground">{m.email}</TableCell>
                  <TableCell>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => removeMutation.mutateAsync({ orgId, userId: m.userId })}
                      disabled={removeMutation.isPending}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function OrgListCard({
  orgs,
  currentUserId,
  onDeleted,
}: {
  orgs: { id: bigint; name: string; ownerId: string }[]
  currentUserId: string
  onDeleted: () => void
}) {
  const deleteMutation = useMutation(deleteOrg, { onSuccess: () => onDeleted() })

  if (orgs.length === 0) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle>Organisations</CardTitle>
        <CardDescription>
          Organisations you belong to. You can delete orgs you own.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Role</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {orgs.map((org) => (
              <TableRow key={String(org.id)}>
                <TableCell className="font-medium">{org.name}</TableCell>
                <TableCell className="text-muted-foreground">
                  {org.ownerId === currentUserId ? 'Owner' : 'Member'}
                </TableCell>
                <TableCell>
                  {org.ownerId === currentUserId && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => deleteMutation.mutateAsync({ id: org.id })}
                      disabled={deleteMutation.isPending}
                    >
                      {deleteMutation.isPending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Trash2 className="h-4 w-4" />
                      )}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
