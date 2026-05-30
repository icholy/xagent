export interface EventTypeOption {
  id: string
  label: string
  source: string
  type: string
}

// Mirrors the (source, type) pairs the webhook handlers actually emit:
//   internal/server/webhookserver/github.go    — type is the X-GitHub-Event header
//   internal/server/webhookserver/atlassian.go — type is the parsed webhookEvent
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
    id: 'atlassian:comment_created',
    label: 'Jira: Issue Comment',
    source: 'atlassian',
    type: 'comment_created',
  },
]

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
