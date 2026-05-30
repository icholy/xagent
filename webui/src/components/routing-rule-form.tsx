import { useMemo, useState } from 'react'
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
import { Loader2 } from 'lucide-react'
import { EVENT_TYPES, findEventType, type EventTypeOption } from '@/lib/routing-rules'

export interface RoutingRuleFormValues {
  source: string
  type: string
  prefix: string
  mention: string
}

export const emptyRoutingRule: RoutingRuleFormValues = {
  source: '',
  type: '',
  prefix: '',
  mention: '',
}

interface MentionCopy {
  label: string
  placeholder: string
  help: string
}

function mentionCopyForSource(source: string): MentionCopy {
  switch (source) {
    case 'github':
      return {
        label: 'Mentions user',
        placeholder: 'octocat',
        help: 'GitHub username (no leading @). Matches @-mentions of this user in the event body.',
      }
    case 'atlassian':
      return {
        label: 'Mentions account',
        placeholder: '5b10ac8d82e05b22cc7d4ef5',
        help: 'Atlassian account ID. The form wraps it as [~accountid:…] when matching — enter just the bare id.',
      }
    default:
      return {
        label: 'Mention',
        placeholder: 'Username or account id',
        help: 'Pick an event type to see the expected format.',
      }
  }
}

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

  // If the rule we're editing carries a (source, type) combination that isn't in the
  // hardcoded list — e.g. a legacy wildcard rule with empty source — expose it as an
  // extra dropdown option so it stays selectable and the stored values are preserved.
  const legacyOption = useMemo<EventTypeOption | null>(() => {
    const isUntouched =
      !initialValues.source &&
      !initialValues.type &&
      !initialValues.prefix &&
      !initialValues.mention
    if (isUntouched) return null
    if (findEventType(initialValues.source, initialValues.type)) return null
    return {
      id: `legacy:${initialValues.source}:${initialValues.type}`,
      label: `Legacy: ${initialValues.source || '(any)'} / ${initialValues.type || '(any)'}`,
      source: initialValues.source,
      type: initialValues.type,
    }
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
  const canSubmit = selectedId !== ''

  const handleEventTypeChange = (id: string) => {
    const known = EVENT_TYPES.find((o) => o.id === id)
    if (known) {
      setValues({ ...values, source: known.source, type: known.type })
      return
    }
    if (legacyOption && legacyOption.id === id) {
      setValues({ ...values, source: legacyOption.source, type: legacyOption.type })
    }
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
        <Label htmlFor="prefix">Message starts with</Label>
        <Input
          id="prefix"
          placeholder="Optional — e.g. /xagent"
          value={values.prefix}
          onChange={(e) => setValues({ ...values, prefix: e.target.value })}
        />
        <p className="text-muted-foreground text-xs">
          Leave blank to match any message. Otherwise the rule only fires when the event body starts
          with this string.
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
