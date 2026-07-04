import { Link2 } from 'lucide-react'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { TaskLink } from '@/gen/xagent/v1/xagent_pb'
import { sourceFromUrl } from '@/lib/timeline'
import type { ExternalSource } from '@/lib/timeline'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { GithubIcon } from '@/components/github-icon'
import { AtlassianIcon } from '@/components/atlassian-icon'

// TaskLinksTab is the read-only Links tab body: a list of the task's links
// (PRs, issues, tickets) with a source icon and a badge marking which are
// subscriptions. It binds directly to getTaskDetails.links — no mutations.
export function TaskLinksTab({ links }: { links: TaskLink[] }) {
  if (links.length === 0) {
    return (
      <div className="p-6 text-sm text-muted-foreground">
        No links yet. The agent adds links (PRs, issues, tickets) with the{' '}
        <code className="rounded bg-muted px-1 py-0.5 text-xs">create_link</code> tool as it works.
      </div>
    )
  }

  // Newest first — mirrors the timeline's mental model (proposal open question 1).
  const ordered = [...links].sort((a, b) => sortKey(b) - sortKey(a))

  return (
    <ul className="divide-y">
      {ordered.map((link) => (
        <LinkRow key={String(link.id)} link={link} />
      ))}
    </ul>
  )
}

function sortKey(link: TaskLink): number {
  return link.createdAt ? timestampDate(link.createdAt).getTime() : 0
}

function sourceIcon(source: ExternalSource) {
  switch (source) {
    case 'github':
      return <GithubIcon className="h-4 w-4" />
    case 'jira':
      return <AtlassianIcon className="h-4 w-4" />
    default:
      return <Link2 className="h-4 w-4" />
  }
}

function LinkRow({ link }: { link: TaskLink }) {
  const source = sourceFromUrl(link.url)
  return (
    <li className="flex items-start gap-3 px-4 py-3">
      <span className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center text-muted-foreground">
        {sourceIcon(source)}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-start gap-2">
          <a
            href={link.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sm font-medium text-primary hover:underline break-words"
          >
            {link.title || link.url}
          </a>
          {link.subscribe && <SubscribedBadge />}
        </div>
        <p className="mt-0.5 break-all text-xs text-muted-foreground">{link.url}</p>
        {link.relevance && <p className="mt-0.5 text-xs text-muted-foreground">{link.relevance}</p>}
      </div>
    </li>
  )
}

function SubscribedBadge() {
  return (
    <Tooltip>
      <TooltipTrigger className="cursor-default">
        <Badge variant="outline" className="border-blue-200 bg-blue-100 text-blue-800 text-[10px]">
          subscribed
        </Badge>
      </TooltipTrigger>
      <TooltipContent>Events on this URL wake the task</TooltipContent>
    </Tooltip>
  )
}
