import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { create } from '@bufbuild/protobuf'
import {
  getRoutingRules,
  setRoutingRules,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { RoutingRuleSchema } from '@/gen/xagent/v1/xagent_pb'
import type { RoutingRule } from '@/gen/xagent/v1/xagent_pb'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Loader2, Pencil, Plus, Save, Trash2, X } from 'lucide-react'

export const Route = createFileRoute('/routing-rules/')({
  component: RoutingRulesPage,
})

const SOURCES = ['github', 'atlassian'] as const

function RoutingRulesPage() {
  const { data, isLoading, error, refetch } = useQuery(getRoutingRules, {}, {
    refetchInterval: 6000,
  })

  const saveMutation = useMutation(setRoutingRules, {
    onSuccess: () => refetch(),
  })

  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [editForm, setEditForm] = useState<RuleFormState>({ source: '', type: '', prefix: '', mention: '' })

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading routing rules...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-destructive">Error: {error.message}</div>
      </div>
    )
  }

  const rules = data?.rules ?? []

  const handleDelete = async (index: number) => {
    const updated = rules.filter((_, i) => i !== index)
    await saveMutation.mutateAsync({ rules: updated })
  }

  const handleStartEdit = (index: number) => {
    const rule = rules[index]
    setEditForm({
      source: rule.source,
      type: rule.type,
      prefix: rule.prefix,
      mention: rule.mention,
    })
    setEditingIndex(index)
  }

  const handleSaveEdit = async () => {
    if (editingIndex === null) return
    const updated = rules.map((rule, i) =>
      i === editingIndex
        ? create(RoutingRuleSchema, {
            source: editForm.source,
            type: editForm.type,
            prefix: editForm.prefix,
            mention: editForm.mention,
          })
        : rule
    )
    await saveMutation.mutateAsync({ rules: updated })
    setEditingIndex(null)
  }

  const handleCancelEdit = () => {
    setEditingIndex(null)
  }

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex flex-wrap items-center justify-between gap-4 mb-6">
        <h1 className="text-2xl font-bold">Routing Rules</h1>
      </div>
      <AddRuleForm
        rules={rules}
        onAdd={refetch}
      />
      {saveMutation.error && (
        <div className="text-destructive text-sm mb-4">
          {saveMutation.error.message}
        </div>
      )}
      {rules.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No routing rules configured
        </div>
      ) : (
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
            {rules.map((rule, index) => (
              editingIndex === index ? (
                <TableRow key={index}>
                  <TableCell>
                    <Select
                      value={editForm.source}
                      onValueChange={(v) => setEditForm({ ...editForm, source: v })}
                    >
                      <SelectTrigger className="w-32">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {SOURCES.map((s) => (
                          <SelectItem key={s} value={s}>{s}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Input
                      value={editForm.type}
                      onChange={(e) => setEditForm({ ...editForm, type: e.target.value })}
                      placeholder="Type"
                      className="w-32"
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      value={editForm.prefix}
                      onChange={(e) => setEditForm({ ...editForm, prefix: e.target.value })}
                      placeholder="Prefix"
                      className="w-48"
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      value={editForm.mention}
                      onChange={(e) => setEditForm({ ...editForm, mention: e.target.value })}
                      placeholder="Mention"
                      className="w-32"
                    />
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleSaveEdit}
                        disabled={saveMutation.isPending || !editForm.source}
                      >
                        {saveMutation.isPending ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <Save className="h-4 w-4" />
                        )}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleCancelEdit}
                      >
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
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleStartEdit(index)}
                        disabled={saveMutation.isPending}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => handleDelete(index)}
                        disabled={saveMutation.isPending}
                      >
                        {saveMutation.isPending ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <Trash2 className="h-4 w-4" />
                        )}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

interface RuleFormState {
  source: string
  type: string
  prefix: string
  mention: string
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
    const newRule = create(RoutingRuleSchema, {
      source: form.source,
      type: form.type,
      prefix: form.prefix,
      mention: form.mention,
    })
    await saveMutation.mutateAsync({ rules: [...rules, newRule] })
  }

  return (
    <form onSubmit={handleSubmit} className="flex gap-2 mb-6 flex-wrap">
      <Select
        value={form.source}
        onValueChange={(v) => setForm({ ...form, source: v })}
      >
        <SelectTrigger className="w-32">
          <SelectValue placeholder="Source" />
        </SelectTrigger>
        <SelectContent>
          {SOURCES.map((s) => (
            <SelectItem key={s} value={s}>{s}</SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        placeholder="Type"
        value={form.type}
        onChange={(e) => setForm({ ...form, type: e.target.value })}
        className="w-32"
      />
      <Input
        placeholder="Prefix"
        value={form.prefix}
        onChange={(e) => setForm({ ...form, prefix: e.target.value })}
        className="w-48"
      />
      <Input
        placeholder="Mention"
        value={form.mention}
        onChange={(e) => setForm({ ...form, mention: e.target.value })}
        className="w-32"
      />
      <Button type="submit" disabled={saveMutation.isPending || !form.source}>
        {saveMutation.isPending ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Plus className="h-4 w-4" />
        )}
        Add Rule
      </Button>
      {saveMutation.error && (
        <span className="text-destructive text-sm self-center">
          {saveMutation.error.message}
        </span>
      )}
    </form>
  )
}
