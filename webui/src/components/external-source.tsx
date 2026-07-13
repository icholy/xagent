import { Bell } from 'lucide-react'
import { GithubIcon } from '@/components/github-icon'
import { AtlassianIcon } from '@/components/atlassian-icon'

// externalSourceStyle maps a source string — the persisted ExternalPayload.source
// for external events, or the URL-inferred source (sourceFromUrl) for links —
// onto the icon, label, and marker classes shown in the timeline and links list.
// github/jira are recognized; anything else (including empty) falls through to a
// generic "External" style.
export function externalSourceStyle(source: string): {
  icon: React.ReactNode
  label: string
  marker: string
} {
  switch (source.toLowerCase()) {
    case 'github':
      return {
        icon: <GithubIcon className="h-4 w-4" />,
        label: 'GitHub',
        marker: 'border-slate-300 bg-slate-100 text-slate-800',
      }
    case 'jira':
      return {
        icon: <AtlassianIcon className="h-4 w-4" />,
        label: 'Jira',
        marker: 'border-blue-300 bg-blue-100 text-blue-700',
      }
    default:
      return {
        icon: <Bell className="h-4 w-4" />,
        label: 'External',
        marker: 'border-border bg-muted text-foreground',
      }
  }
}
