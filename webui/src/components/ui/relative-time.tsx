import { Duration } from '@icholy/duration'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

function formatDuration(duration: Duration): string {
  const truncatedDuration = duration.isGreaterThan('1h')
    ? duration.truncate('1m')
    : duration.truncate('1s')
  return truncatedDuration.toString()
}

export function RelativeTime({ date }: { date: Date }) {
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const duration = new Duration(diff)

  const relativeText =
    diff < 1000 ? 'just now' : `${formatDuration(duration)} ago`
  const absoluteText = date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })

  return (
    <Tooltip>
      <TooltipTrigger className="cursor-default">
        {relativeText}
      </TooltipTrigger>
      <TooltipContent>
        {absoluteText}
      </TooltipContent>
    </Tooltip>
  )
}
