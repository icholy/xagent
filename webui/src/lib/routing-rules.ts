import { create } from '@bufbuild/protobuf'
import { type EventTypeDef, type RoutingRule, RoutingRuleSchema } from '@/gen/xagent/v1/xagent_pb'
import { durationFromHours, hoursFromDuration } from '@/lib/duration'

// The condition operators the backend understands. Op semantics are literal
// (exact, case-sensitive) string comparisons — see RuleCondition in the proto.
export const CONDITION_OPS = ['equals', 'prefix', 'contains'] as const
export type ConditionOp = (typeof CONDITION_OPS)[number]

export const OP_LABELS: Record<ConditionOp, string> = {
  equals: 'equals',
  prefix: 'starts with',
  contains: 'contains',
}

// Stable id for a (source, type) event-type pair, used as the dropdown value.
export function eventTypeId(source: string, type: string): string {
  return `${source}:${type}`
}

// Looks up a fetched event-type definition by (source, type).
export function findEventType(
  eventTypes: EventTypeDef[],
  source: string,
  type: string,
): EventTypeDef | undefined {
  return eventTypes.find((t) => t.source === source && t.type === type)
}

// Friendly label for a stored (source, type) combo. Falls back to a "Legacy: …"
// label for rules whose combo isn't in the registry (e.g. a wildcard rule or a
// type emitted by a newer/older server) so the value is still readable.
export function eventTypeLabel(eventTypes: EventTypeDef[], source: string, type: string): string {
  const known = findEventType(eventTypes, source, type)
  if (known) return known.label
  return `Legacy: ${source || '(any)'} / ${type || '(any)'}`
}

// Synthetic event-type definition for a stored (source, type) combo that isn't
// in the registry, so legacy rules stay selectable. Returns null when the combo
// is already a known event type. Its attrs are the union of well-known attrs so
// the condition editor can still edit the rule's existing conditions.
export function legacyEventType(
  eventTypes: EventTypeDef[],
  source: string,
  type: string,
): EventTypeDef | null {
  if (findEventType(eventTypes, source, type)) return null
  return {
    $typeName: 'xagent.v1.EventTypeDef',
    source,
    type,
    label: eventTypeLabel(eventTypes, source, type),
    attrs: [...ALL_ATTRS],
  }
}

// Well-known attribute dimensions. `body` and `url` are derived views over
// every event; the rest are emitted per event type. Kept as the fallback attr
// set for legacy rules whose event type isn't in the registry.
export const ALL_ATTRS = ['body', 'url', 'mention', 'assignee', 'label', 'state'] as const

export interface AttrCopy {
  label: string
  placeholder: string
  help: string
}

// Per-attr display copy for a condition row, replacing the old per-event-type
// field copy (mentionCopyForSource etc.). Some attrs read differently per
// source, so the selected event type's source is threaded through.
export function attrCopy(attr: string, source: string): AttrCopy {
  switch (attr) {
    case 'body':
      return {
        label: 'Body',
        placeholder: 'xagent:',
        help: 'Matched against the event body — the comment or description text.',
      }
    case 'url':
      return {
        label: 'URL',
        placeholder:
          source === 'atlassian'
            ? 'https://your-domain.atlassian.net/browse/PROJ-'
            : 'https://github.com/owner/repo/',
        help: 'Matched against the event URL — e.g. to scope a rule to a single repo or project.',
      }
    case 'mention':
      if (source === 'atlassian') {
        return {
          label: 'Mention',
          placeholder: '5b10ac8d82e05b22cc7d4ef5',
          help: 'Atlassian account id mentioned in the event body. Enter the bare id (no [~accountid:…] wrapper).',
        }
      }
      return {
        label: 'Mention',
        placeholder: 'octocat',
        help: 'GitHub username mentioned in the event body (no leading @).',
      }
    case 'assignee':
      return {
        label: 'Assignee',
        placeholder: 'icholy-bot',
        help: 'The new assignee on an assignment event (GitHub username, no leading @).',
      }
    case 'label':
      return {
        label: 'Label',
        placeholder: 'xagent',
        help: 'A label added to the issue or PR.',
      }
    case 'state':
      return {
        label: 'State',
        placeholder: 'merged',
        help: 'The resulting state — e.g. "merged" or "closed" for a closed PR.',
      }
    default:
      return {
        label: attr,
        placeholder: '',
        help: 'Value the condition matches against.',
      }
  }
}

export interface ConditionDraft {
  attr: string
  op: string
  value: string
}

// Form-level shape for the routing-rule editor. Mirrors the RoutingRule proto
// fields plus a `createTask` toggle and flattened CreateTaskAction fields so
// the form can keep its draft state across toggling the action off and on.
export interface RoutingRuleFormValues {
  source: string
  type: string
  conditions: ConditionDraft[]
  // Whether a matched rule wakes (restarts) the linked task(s). Defaults to
  // true for new rules; unchecking it sends wakeup: false.
  wakeup: boolean
  createTask: boolean
  createWorkspace: string
  createRunner: string
  createPrompt: string
  // Whole-hours string ('' / 'never' = never) for the created task's
  // auto-archive timeout. Matches the Create Task screen's control.
  createAutoArchive: string
}

export const emptyRoutingRule: RoutingRuleFormValues = {
  source: '',
  type: '',
  conditions: [],
  wakeup: true,
  createTask: false,
  createWorkspace: '',
  createRunner: '',
  createPrompt: '',
  createAutoArchive: '',
}

export function formValuesFromRoutingRule(rule: RoutingRule): RoutingRuleFormValues {
  return {
    source: rule.source,
    type: rule.type,
    conditions: rule.conditions.map((c) => ({ attr: c.attr, op: c.op, value: c.value })),
    wakeup: rule.wakeup,
    createTask: rule.create !== undefined,
    createWorkspace: rule.create?.workspace ?? '',
    createRunner: rule.create?.runner ?? '',
    createPrompt: rule.create?.prompt ?? '',
    createAutoArchive: hoursFromDuration(rule.create?.autoArchive),
  }
}

// Builds a RoutingRule from the form's draft values. `create` is only set when
// the toggle is on — otherwise it's omitted so the rule reverts to the wake-only
// behaviour. Conditions with a blank attr are dropped (incomplete draft rows).
export function buildRoutingRule(values: RoutingRuleFormValues): RoutingRule {
  return create(RoutingRuleSchema, {
    source: values.source,
    type: values.type,
    conditions: values.conditions
      .filter((c) => c.attr.trim() !== '')
      .map((c) => ({ attr: c.attr, op: c.op, value: c.value })),
    wakeup: values.wakeup,
    create: values.createTask
      ? {
          workspace: values.createWorkspace,
          runner: values.createRunner,
          prompt: values.createPrompt,
          autoArchive: durationFromHours(values.createAutoArchive),
        }
      : undefined,
  })
}

// True when the form has the minimum fields needed to submit. Event type must
// be selected; every condition must target an attr; if the create-task toggle
// is on, workspace and runner must both be chosen.
export function isRoutingRuleFormValid(
  values: RoutingRuleFormValues,
  eventTypeSelected: boolean,
): boolean {
  if (!eventTypeSelected) return false
  if (values.conditions.some((c) => c.attr.trim() === '')) return false
  if (values.createTask) {
    if (!values.createRunner.trim() || !values.createWorkspace.trim()) return false
  }
  return true
}
