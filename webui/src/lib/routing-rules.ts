import { create } from '@bufbuild/protobuf'
import { type RoutingRule, RoutingRuleSchema } from '@/gen/xagent/v1/xagent_pb'
import { durationFromHours, hoursFromDuration } from '@/lib/duration'

export interface EventTypeOption {
  id: string
  label: string
  source: string
  type: string
}

// Mirrors the (source, type) pairs the webhook handlers actually emit:
//   internal/server/githubserver/webhook.go       — type is the X-GitHub-Event header
//   internal/server/atlassianserver/webhook.go    — type is the parsed webhookEvent
export const EVENT_TYPES: EventTypeOption[] = [
  {
    id: 'github:issue_comment',
    label: 'GitHub: Issue/PR Comment',
    source: 'github',
    type: 'issue_comment',
  },
  {
    id: 'github:pull_request_review_comment',
    label: 'GitHub: PR Review Comment',
    source: 'github',
    type: 'pull_request_review_comment',
  },
  {
    id: 'github:pull_request_review',
    label: 'GitHub: PR Review',
    source: 'github',
    type: 'pull_request_review',
  },
  {
    id: 'github:issue_assigned',
    label: 'GitHub: Issue Assigned',
    source: 'github',
    type: 'issue_assigned',
  },
  {
    id: 'github:pull_request_assigned',
    label: 'GitHub: PR Assigned',
    source: 'github',
    type: 'pull_request_assigned',
  },
  {
    id: 'atlassian:comment_created',
    label: 'Jira: Issue Comment',
    source: 'atlassian',
    type: 'comment_created',
  },
]

// Whether the selected event type is an assignment event — controls
// visibility of the "Assigned to" form field.
export function isAssignmentType(source: string, type: string): boolean {
  return source === 'github' && (type === 'issue_assigned' || type === 'pull_request_assigned')
}

export function findEventType(source: string, type: string): EventTypeOption | undefined {
  return EVENT_TYPES.find((o) => o.source === source && o.type === type)
}

export function findEventTypeById(id: string): EventTypeOption | undefined {
  return EVENT_TYPES.find((o) => o.id === id)
}

// Friendly label for a stored (source, type) combo. Falls back to a "Legacy: …"
// label for rules whose combo isn't in EVENT_TYPES (e.g. legacy wildcard rules
// or types emitted by an older server) so the value is still readable.
export function eventTypeLabel(source: string, type: string): string {
  const known = findEventType(source, type)
  if (known) return known.label
  return `Legacy: ${source || '(any)'} / ${type || '(any)'}`
}

// Synthetic dropdown option that represents a (source, type) combo not in
// EVENT_TYPES — used by the form to keep legacy rules selectable. Returns
// null when the combo is already a known event type.
export function legacyEventTypeOption(source: string, type: string): EventTypeOption | null {
  if (findEventType(source, type)) return null
  return {
    id: `legacy:${source}:${type}`,
    label: eventTypeLabel(source, type),
    source,
    type,
  }
}

export interface MentionCopy {
  label: string
  placeholder: string
  help: string
}

export interface AssigneeCopy {
  label: string
  placeholder: string
  help: string
}

export interface URLPrefixCopy {
  placeholder: string
  help: string
}

// Form-level shape for the routing-rule editor. Mirrors the RoutingRule proto
// fields plus a `createTask` toggle and flattened CreateTaskAction fields so
// the form can keep its draft state across toggling the action off and on.
export interface RoutingRuleFormValues {
  source: string
  type: string
  prefix: string
  mention: string
  assignee: string
  urlPrefix: string
  createTask: boolean
  createWorkspace: string
  createRunner: string
  createPrompt: string
  // Whole-hours string ('' / 'never' = never) for the created task's
  // auto-archive timeout. Matches the Create Task screen's control.
  createArchiveAfter: string
}

export const emptyRoutingRule: RoutingRuleFormValues = {
  source: '',
  type: '',
  prefix: '',
  mention: '',
  assignee: '',
  urlPrefix: '',
  createTask: false,
  createWorkspace: '',
  createRunner: '',
  createPrompt: '',
  createArchiveAfter: '',
}

export function formValuesFromRoutingRule(rule: RoutingRule): RoutingRuleFormValues {
  return {
    source: rule.source,
    type: rule.type,
    prefix: rule.prefix,
    mention: rule.mention,
    assignee: rule.assignee,
    urlPrefix: rule.urlPrefix,
    createTask: rule.create !== undefined,
    createWorkspace: rule.create?.workspace ?? '',
    createRunner: rule.create?.runner ?? '',
    createPrompt: rule.create?.prompt ?? '',
    createArchiveAfter: hoursFromDuration(rule.create?.archiveAfter),
  }
}

// Builds a RoutingRule from the form's draft values. `create` is only set
// when the toggle is on — otherwise it's omitted so the rule reverts to the
// wake-only behaviour. Assignment events have no message body, so when the
// selected event type is an assignment one we drop prefix/mention; for
// non-assignment types we drop assignee. This keeps the form's draft state
// usable across event-type toggles without persisting fields the rule can't
// actually match on.
export function buildRoutingRule(values: RoutingRuleFormValues): RoutingRule {
  const isAssignment = isAssignmentType(values.source, values.type)
  return create(RoutingRuleSchema, {
    source: values.source,
    type: values.type,
    prefix: isAssignment ? '' : values.prefix,
    mention: isAssignment ? '' : values.mention,
    assignee: isAssignment ? values.assignee : '',
    urlPrefix: values.urlPrefix,
    create: values.createTask
      ? {
          workspace: values.createWorkspace,
          runner: values.createRunner,
          prompt: values.createPrompt,
          archiveAfter: durationFromHours(values.createArchiveAfter),
        }
      : undefined,
  })
}

// True when the form has the minimum fields needed to submit. Event type
// must be selected; if the create-task toggle is on, workspace and runner
// must both be chosen.
export function isRoutingRuleFormValid(
  values: RoutingRuleFormValues,
  eventTypeSelected: boolean,
): boolean {
  if (!eventTypeSelected) return false
  if (values.createTask) {
    if (!values.createRunner.trim() || !values.createWorkspace.trim()) return false
  }
  return true
}

export function mentionCopyForSource(source: string): MentionCopy {
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

export function urlPrefixCopyForSource(source: string): URLPrefixCopy {
  switch (source) {
    case 'github':
      return {
        placeholder: 'https://github.com/owner/repo/',
        help: 'Optional. Only fire when the event URL starts with this prefix — e.g. to scope a rule to a single repo.',
      }
    case 'atlassian':
      return {
        placeholder: 'https://your-domain.atlassian.net/browse/PROJ-',
        help: 'Optional. Only fire when the event URL starts with this prefix — e.g. to scope a rule to a single Jira project.',
      }
    default:
      return {
        placeholder: 'https://...',
        help: 'Optional. Only fire when the event URL starts with this prefix.',
      }
  }
}

export function assigneeCopyForSource(source: string): AssigneeCopy {
  switch (source) {
    case 'github':
      return {
        label: 'Assigned to user',
        placeholder: 'icholy-bot',
        help: 'GitHub username (no leading @). Matches the new assignee on assignment events.',
      }
    default:
      return {
        label: 'Assigned to',
        placeholder: 'Username or account id',
        help: 'Pick an assignment event type to see the expected format.',
      }
  }
}
