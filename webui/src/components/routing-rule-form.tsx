import { useMemo, useState } from 'react'
import { useQuery } from '@connectrpc/connect-query'
import { listWorkspaces } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Loader2 } from 'lucide-react'
import {
  assigneeCopyForSource,
  EVENT_TYPES,
  emptyRoutingRule,
  findEventType,
  findEventTypeById,
  isAssignmentType,
  isRoutingRuleFormValid,
  legacyEventTypeOption,
  mentionCopyForSource,
  urlPrefixCopyForSource,
  type RoutingRuleFormValues,
} from '@/lib/routing-rules'

export { emptyRoutingRule, type RoutingRuleFormValues }

interface RoutingRuleFormProps {
  initialValues: RoutingRuleFormValues
  submitLabel: string
  isSubmitting?: boolean
  error?: string | null
  onSubmit: (values: RoutingRuleFormValues) => void | Promise<void>
  onCancel: () => void
}

export function RoutingRuleForm({
  initialValues,
  submitLabel,
  isSubmitting,
  error,
  onSubmit,
  onCancel,
}: RoutingRuleFormProps) {
  const [values, setValues] = useState<RoutingRuleFormValues>(initialValues)

  // For a brand-new rule (no fields set), suppress the synthetic legacy option so
  // the user doesn't see a "Legacy: (any) / (any)" entry in the dropdown.
  const legacyOption = useMemo(() => {
    const isUntouched =
      !initialValues.source &&
      !initialValues.type &&
      !initialValues.prefix &&
      !initialValues.mention &&
      !initialValues.assignee &&
      !initialValues.urlPrefix
    if (isUntouched) return null
    return legacyEventTypeOption(initialValues.source, initialValues.type)
  }, [initialValues])

  const selectedId = useMemo(() => {
    const known = findEventType(values.source, values.type)
    if (known) return known.id
    if (
      legacyOption &&
      legacyOption.source === values.source &&
      legacyOption.type === values.type
    ) {
      return legacyOption.id
    }
    return ''
  }, [values.source, values.type, legacyOption])

  const mentionCopy = mentionCopyForSource(values.source)
  const assigneeCopy = assigneeCopyForSource(values.source)
  const urlPrefixCopy = urlPrefixCopyForSource(values.source)
  // Assignment events have no message body, so prefix/mention can't match —
  // hide those fields and show the assignee field instead.
  const isAssignment = isAssignmentType(values.source, values.type)
  const canSubmit = isRoutingRuleFormValid(values, selectedId !== '')

  const { data: workspacesData } = useQuery(listWorkspaces, {}, { enabled: values.createTask })
  const runners = useMemo(
    () => [...new Set(workspacesData?.workspaces.map((ws) => ws.runnerId) ?? [])],
    [workspacesData],
  )
  const workspaces = useMemo(
    () => workspacesData?.workspaces.filter((ws) => ws.runnerId === values.createRunner) ?? [],
    [workspacesData, values.createRunner],
  )

  const handleEventTypeChange = (id: string) => {
    const known = findEventTypeById(id)
    if (known) {
      setValues({ ...values, source: known.source, type: known.type })
      return
    }
    if (legacyOption && legacyOption.id === id) {
      setValues({ ...values, source: legacyOption.source, type: legacyOption.type })
    }
  }

  const handleCreateRunnerChange = (newRunner: string) => {
    // Clear the workspace when the runner changes — workspaces are filtered
    // per-runner and the previous selection likely won't apply.
    setValues({ ...values, createRunner: newRunner, createWorkspace: '' })
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    void onSubmit(values)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="space-y-2">
        <Label htmlFor="event-type">Event type</Label>
        <Select value={selectedId} onValueChange={handleEventTypeChange} required>
          <SelectTrigger id="event-type">
            <SelectValue placeholder="Select an event type" />
          </SelectTrigger>
          <SelectContent>
            {EVENT_TYPES.map((o) => (
              <SelectItem key={o.id} value={o.id}>
                {o.label}
              </SelectItem>
            ))}
            {legacyOption && <SelectItem value={legacyOption.id}>{legacyOption.label}</SelectItem>}
          </SelectContent>
        </Select>
        <p className="text-muted-foreground text-xs">
          The kind of incoming webhook event this rule applies to.
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="url-prefix">URL prefix</Label>
        <Input
          id="url-prefix"
          placeholder={urlPrefixCopy.placeholder}
          value={values.urlPrefix}
          onChange={(e) => setValues({ ...values, urlPrefix: e.target.value })}
        />
        <p className="text-muted-foreground text-xs">{urlPrefixCopy.help}</p>
      </div>

      {!isAssignment && (
        <>
          <div className="space-y-2">
            <Label htmlFor="prefix">Message starts with</Label>
            <Input
              id="prefix"
              placeholder="Optional — e.g. /xagent"
              value={values.prefix}
              onChange={(e) => setValues({ ...values, prefix: e.target.value })}
            />
            <p className="text-muted-foreground text-xs">
              Leave blank to match any message. Otherwise the rule only fires when the event body
              starts with this string.
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="mention">{mentionCopy.label}</Label>
            <Input
              id="mention"
              placeholder={mentionCopy.placeholder}
              value={values.mention}
              onChange={(e) => setValues({ ...values, mention: e.target.value })}
            />
            <p className="text-muted-foreground text-xs">{mentionCopy.help}</p>
          </div>
        </>
      )}

      {isAssignment && (
        <div className="space-y-2">
          <Label htmlFor="assignee">{assigneeCopy.label}</Label>
          <Input
            id="assignee"
            placeholder={assigneeCopy.placeholder}
            value={values.assignee}
            onChange={(e) => setValues({ ...values, assignee: e.target.value })}
          />
          <p className="text-muted-foreground text-xs">{assigneeCopy.help}</p>
        </div>
      )}

      <div className="space-y-4 rounded-md border p-4">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label htmlFor="create-task">Create a task</Label>
            <p className="text-muted-foreground text-xs">
              When the rule matches and no subscribed task is found, create a new task in the
              selected workspace.
            </p>
          </div>
          <Switch
            id="create-task"
            checked={values.createTask}
            onCheckedChange={(checked) => setValues({ ...values, createTask: checked })}
          />
        </div>

        {values.createTask && (
          <div className="space-y-4 border-t pt-4">
            <div className="space-y-2">
              <Label htmlFor="create-runner">Runner</Label>
              <Select value={values.createRunner} onValueChange={handleCreateRunnerChange} required>
                <SelectTrigger id="create-runner">
                  <SelectValue placeholder="Select a runner" />
                </SelectTrigger>
                <SelectContent>
                  {runners.map((r) => (
                    <SelectItem key={r} value={r}>
                      {r}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="create-workspace">Workspace</Label>
              <Select
                value={values.createWorkspace}
                onValueChange={(v) => setValues({ ...values, createWorkspace: v })}
                required
                disabled={!values.createRunner}
              >
                <SelectTrigger id="create-workspace">
                  <SelectValue
                    placeholder={
                      values.createRunner ? 'Select a workspace' : 'Select a runner first'
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {workspaces.map((ws) => (
                    <SelectItem key={ws.name} value={ws.name}>
                      <span>{ws.name}</span>
                      {ws.description && (
                        <span className="text-muted-foreground text-xs ml-2">{ws.description}</span>
                      )}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="create-prompt">Prompt (optional)</Label>
              <Textarea
                id="create-prompt"
                placeholder="Initial instruction for the created task..."
                value={values.createPrompt}
                onChange={(e) => setValues({ ...values, createPrompt: e.target.value })}
                rows={4}
              />
              <p className="text-muted-foreground text-xs">
                Leave blank to use the event body as the task's first instruction.
              </p>
            </div>
          </div>
        )}
      </div>

      {error && <div className="text-destructive text-sm">{error}</div>}

      <div className="flex gap-2">
        <Button type="submit" disabled={isSubmitting || !canSubmit}>
          {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {submitLabel}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
