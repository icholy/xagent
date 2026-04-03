import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { create } from '@bufbuild/protobuf'
import {
  getProfile,
  unlinkGitHubAccount,
  unlinkAtlassianAccount,
  createOrg,
  deleteOrg,
  getOrgSettings,
  generateAtlassianWebhookSecret,
  getRoutingRules,
  setRoutingRules,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Org, RoutingRule } from '@/gen/xagent/v1/xagent_pb'
import { RoutingRuleSchema } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { RelativeTime } from '@/components/relative-time'
import { Cable, Check, Copy, ExternalLink, Github, KeyRound, Loader2, Mail, Pencil, Plus, RefreshCw, Save, Trash2, Unlink, User, X } from 'lucide-react'

export const Route = createFileRoute('/settings')({
  component: SettingsPage,
  validateSearch: (search: Record<string, unknown>): { tab: string } => ({
    tab: search.tab === 'organisation' ? 'organisation' : 'account',
  }),
})

function SettingsPage() {
  const { tab } = Route.useSearch()
  const navigate = useNavigate()

  return (
    <div className="container mx-auto py-8 px-4">
      <h1 className="text-2xl font-bold mb-6">Settings</h1>
      <Tabs value={tab} onValueChange={(value) => navigate({ search: { tab: value }, replace: true })}>
        <div className="flex items-center mb-4">
          <ProfileCard />
          <TabsList className="ml-auto">
            <TabsTrigger value="account">Account</TabsTrigger>
            <TabsTrigger value="organisation">Organisation</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="account">
          <AccountSettings />
        </TabsContent>
        <TabsContent value="organisation">
          <OrgSettings />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function AccountSettings() {
  const { data, isLoading, refetch } = useQuery(getProfile, {})
  const unlinkMutation = useMutation(unlinkGitHubAccount, {
    onSuccess: () => refetch(),
  })

  const account = data?.githubAccount

  return (
    <div className="space-y-6">
      <OrgsCard />
      <AtlassianAccountCard />
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
        </CardContent>
      </Card>
    </div>
  )
}

function OrgSettings() {
  const { data, isLoading, refetch } = useQuery(getOrgSettings, {})
  const generateMutation = useMutation(generateAtlassianWebhookSecret, {
    onSuccess: () => refetch(),
  })
  const [copied, setCopied] = useState<'secret' | 'url' | null>(null)

  const copyToClipboard = (text: string, field: 'secret' | 'url') => {
    navigator.clipboard.writeText(text)
    setCopied(field)
    setTimeout(() => setCopied(null), 2000)
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>MCP Server</CardTitle>
          <CardDescription>
            xagent provides an MCP server that you can connect to from any MCP-compatible client.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="text-muted-foreground">Loading...</div>
          ) : data?.mcpUrl ? (
            <div className="flex items-center gap-2">
              <Cable className="h-5 w-5 text-muted-foreground" />
              <code className="text-sm bg-muted px-2 py-1 rounded">{data.mcpUrl}</code>
            </div>
          ) : null}
        </CardContent>
      </Card>
      {data?.githubAppUrl && (
        <Card>
          <CardHeader>
            <CardTitle>GitHub App</CardTitle>
            <CardDescription>
              Install the GitHub App to receive webhook notifications for pull requests on your tasks.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <a
              href={data.githubAppUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button variant="outline">
                <ExternalLink className="h-4 w-4" />
                Install GitHub App
              </Button>
            </a>
          </CardContent>
        </Card>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Atlassian Webhook</CardTitle>
          <CardDescription>
            Configure a webhook secret to receive Atlassian events (e.g. Jira issue comments) for your tasks.
            Register this webhook URL in your Atlassian admin settings.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="text-muted-foreground">Loading...</div>
          ) : (
            <>
              <div className="space-y-3">
                <div>
                  <label className="text-sm font-medium">Webhook URL</label>
                  <div className="flex items-center gap-2 mt-1">
                    <code className="text-sm bg-muted px-2 py-1 rounded flex-1 truncate">
                      {data?.atlassianWebhookUrl || '—'}
                    </code>
                    {data?.atlassianWebhookUrl && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => copyToClipboard(data.atlassianWebhookUrl, 'url')}
                      >
                        {copied === 'url' ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                      </Button>
                    )}
                  </div>
                </div>
                <div>
                  <label className="text-sm font-medium">Secret</label>
                  <div className="flex items-center gap-2 mt-1">
                    {data?.atlassianWebhookSecret ? (
                      <>
                        <code className="text-sm bg-muted px-2 py-1 rounded flex-1 truncate">
                          {data.atlassianWebhookSecret.slice(0, 8)}{'•'.repeat(24)}
                        </code>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => copyToClipboard(data.atlassianWebhookSecret, 'secret')}
                        >
                          {copied === 'secret' ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                        </Button>
                      </>
                    ) : (
                      <span className="text-sm text-muted-foreground">No secret configured</span>
                    )}
                  </div>
                </div>
              </div>
              <Button
                onClick={() => generateMutation.mutateAsync({})}
                disabled={generateMutation.isPending}
              >
                {generateMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : data?.atlassianWebhookSecret ? (
                  <RefreshCw className="h-4 w-4" />
                ) : (
                  <KeyRound className="h-4 w-4" />
                )}
                {data?.atlassianWebhookSecret ? 'Regenerate Secret' : 'Generate Secret'}
              </Button>
            </>
          )}
        </CardContent>
      </Card>
      <RoutingRulesCard />
    </div>
  )
}

const ROUTING_SOURCES = ['github', 'atlassian'] as const

interface RuleFormState {
  source: string
  type: string
  prefix: string
  mention: string
}

function RoutingRulesCard() {
  const { data, isLoading, refetch } = useQuery(getRoutingRules, {}, {
    refetchInterval: 6000,
  })
  const saveMutation = useMutation(setRoutingRules, {
    onSuccess: () => refetch(),
  })
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [editForm, setEditForm] = useState<RuleFormState>({ source: '', type: '', prefix: '', mention: '' })

  const rules = data?.rules ?? []

  const handleDelete = async (index: number) => {
    const updated = rules.filter((_, i) => i !== index)
    await saveMutation.mutateAsync({ rules: updated })
  }

  const handleStartEdit = (index: number) => {
    const rule = rules[index]
    setEditForm({ source: rule.source, type: rule.type, prefix: rule.prefix, mention: rule.mention })
    setEditingIndex(index)
  }

  const handleSaveEdit = async () => {
    if (editingIndex === null) return
    const updated = rules.map((rule, i) =>
      i === editingIndex
        ? create(RoutingRuleSchema, { source: editForm.source, type: editForm.type, prefix: editForm.prefix, mention: editForm.mention })
        : rule
    )
    await saveMutation.mutateAsync({ rules: updated })
    setEditingIndex(null)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Routing Rules</CardTitle>
        <CardDescription>
          Configure how events get routed to tasks and workspaces.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {isLoading ? (
          <div className="text-muted-foreground">Loading...</div>
        ) : (
          <>
            <AddRuleForm rules={rules} onAdd={refetch} />
            {saveMutation.error && (
              <div className="text-destructive text-sm">{saveMutation.error.message}</div>
            )}
            {rules.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Source</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Prefix</TableHead>
                    <TableHead>Mention</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rules.map((rule, index) =>
                    editingIndex === index ? (
                      <TableRow key={index}>
                        <TableCell>
                          <Select value={editForm.source} onValueChange={(v) => setEditForm({ ...editForm, source: v })}>
                            <SelectTrigger className="w-32">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {ROUTING_SOURCES.map((s) => (
                                <SelectItem key={s} value={s}>{s}</SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </TableCell>
                        <TableCell>
                          <Input value={editForm.type} onChange={(e) => setEditForm({ ...editForm, type: e.target.value })} placeholder="Type" className="w-32" />
                        </TableCell>
                        <TableCell>
                          <Input value={editForm.prefix} onChange={(e) => setEditForm({ ...editForm, prefix: e.target.value })} placeholder="Prefix" className="w-48" />
                        </TableCell>
                        <TableCell>
                          <Input value={editForm.mention} onChange={(e) => setEditForm({ ...editForm, mention: e.target.value })} placeholder="Mention" className="w-32" />
                        </TableCell>
                        <TableCell>
                          <div className="flex gap-1">
                            <Button variant="outline" size="sm" onClick={handleSaveEdit} disabled={saveMutation.isPending || !editForm.source}>
                              {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                            </Button>
                            <Button variant="outline" size="sm" onClick={() => setEditingIndex(null)}>
                              <X className="h-4 w-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ) : (
                      <TableRow key={index}>
                        <TableCell className="font-medium">{rule.source}</TableCell>
                        <TableCell className="text-muted-foreground">{rule.type || '-'}</TableCell>
                        <TableCell className="text-muted-foreground">{rule.prefix || '-'}</TableCell>
                        <TableCell className="text-muted-foreground">{rule.mention || '-'}</TableCell>
                        <TableCell>
                          <div className="flex gap-1">
                            <Button variant="outline" size="sm" onClick={() => handleStartEdit(index)} disabled={saveMutation.isPending}>
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button variant="destructive" size="sm" onClick={() => handleDelete(index)} disabled={saveMutation.isPending}>
                              {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  )}
                </TableBody>
              </Table>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}

function AddRuleForm({ rules, onAdd }: { rules: RoutingRule[]; onAdd: () => void }) {
  const [form, setForm] = useState<RuleFormState>({ source: '', type: '', prefix: '', mention: '' })
  const saveMutation = useMutation(setRoutingRules, {
    onSuccess: () => {
      setForm({ source: '', type: '', prefix: '', mention: '' })
      onAdd()
    },
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.source) return
    const newRule = create(RoutingRuleSchema, { source: form.source, type: form.type, prefix: form.prefix, mention: form.mention })
    await saveMutation.mutateAsync({ rules: [...rules, newRule] })
  }

  return (
    <form onSubmit={handleSubmit} className="flex gap-2 flex-wrap">
      <Select value={form.source} onValueChange={(v) => setForm({ ...form, source: v })}>
        <SelectTrigger className="w-32">
          <SelectValue placeholder="Source" />
        </SelectTrigger>
        <SelectContent>
          {ROUTING_SOURCES.map((s) => (
            <SelectItem key={s} value={s}>{s}</SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input placeholder="Type" value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} className="w-32" />
      <Input placeholder="Prefix" value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} className="w-48" />
      <Input placeholder="Mention" value={form.mention} onChange={(e) => setForm({ ...form, mention: e.target.value })} className="w-32" />
      <Button type="submit" disabled={saveMutation.isPending || !form.source}>
        {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
        Add Rule
      </Button>
      {saveMutation.error && (
        <span className="text-destructive text-sm self-center">{saveMutation.error.message}</span>
      )}
    </form>
  )
}

function ProfileCard() {
  const { data: profileData } = useQuery(getProfile, {})
  const profile = profileData?.profile

  if (!profile) return null

  return (
    <div className="flex items-center gap-4 text-sm">
      <div className="flex items-center gap-1.5">
        <User className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{profile.name}</span>
      </div>
      <div className="hidden md:flex items-center gap-1.5 text-muted-foreground">
        <Mail className="h-4 w-4" />
        <span>{profile.email}</span>
      </div>
    </div>
  )
}

function OrgsCard() {
  const { data: profileData, refetch } = useQuery(getProfile, {})
  const userId = profileData?.profile?.id
  const orgs = (profileData?.orgs ?? []).filter((org) => org.owner === userId)

  return (
    <Card>
      <CardHeader>
        <CardTitle>My Organisations</CardTitle>
        <CardDescription>
          Create and manage your organisations.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <CreateOrgForm onCreate={refetch} />
        {orgs.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Created</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {orgs.map((org) => (
                <OrgRow key={String(org.id)} org={org} onDelete={refetch} isDefault={org.id === profileData?.defaultOrgId} />
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function CreateOrgForm({ onCreate }: { onCreate: () => void }) {
  const [name, setName] = useState('')
  const mutation = useMutation(createOrg, {
    onSuccess: () => {
      setName('')
      onCreate()
    },
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    await mutation.mutateAsync({ name: name.trim() })
  }

  return (
    <form onSubmit={handleSubmit} className="flex gap-2">
      <Input
        type="text"
        placeholder="Organisation name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="max-w-sm"
      />
      <Button type="submit" disabled={mutation.isPending || !name.trim()}>
        {mutation.isPending ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Plus className="h-4 w-4" />
        )}
        Create
      </Button>
      {mutation.error && (
        <span className="text-destructive text-sm self-center">
          {mutation.error.message}
        </span>
      )}
    </form>
  )
}

function OrgRow({ org, onDelete, isDefault }: { org: Org; onDelete: () => void; isDefault: boolean }) {
  const deleteMutation = useMutation(deleteOrg, {
    onSuccess: () => onDelete(),
  })

  return (
    <TableRow>
      <TableCell className="text-muted-foreground">{String(org.id)}</TableCell>
      <TableCell className="font-medium">{org.name}</TableCell>
      <TableCell className="text-muted-foreground">
        {org.createdAt ? <RelativeTime date={timestampDate(org.createdAt)} /> : '-'}
      </TableCell>
      <TableCell>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => deleteMutation.mutateAsync({ id: org.id })}
          disabled={deleteMutation.isPending || isDefault}
        >
          {deleteMutation.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Trash2 className="h-4 w-4" />
          )}
          Delete
        </Button>
      </TableCell>
    </TableRow>
  )
}

function AtlassianAccountCard() {
  const { data, isLoading, refetch } = useQuery(getProfile, {})
  const unlinkMutation = useMutation(unlinkAtlassianAccount, {
    onSuccess: () => refetch(),
  })

  const account = data?.atlassianAccount

  return (
    <Card>
      <CardHeader>
        <CardTitle>Atlassian Account</CardTitle>
        <CardDescription>
          Link your Atlassian account to receive notifications for Jira issues on your tasks.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="text-muted-foreground">Loading...</div>
        ) : account ? (
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <User className="h-5 w-5" />
              <span className="font-medium">{account.atlassianUsername || account.atlassianAccountId}</span>
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
          <a href="/atlassian/login">
            <Button>
              <ExternalLink className="h-4 w-4" />
              Link Atlassian Account
            </Button>
          </a>
        )}
      </CardContent>
    </Card>
  )
}

