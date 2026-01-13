import { Duration } from '@icholy/duration'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

function formatDuration(duration: Duration): string {
  if (duration.isLessThan('1s')) {
    return 'just now'
  }
  if (duration.isGreaterThan('1h')) {
    return `${duration.truncate('1m').toString()} ago`
  }
  return `${duration.truncate('1s').toString()} ago`
}

export function RelativeTime({ date }: { date: Date }) {
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const duration = new Duration(diff)

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
        {formatDuration(duration)}
      </TooltipTrigger>
      <TooltipContent>
        {absoluteText}
      </TooltipContent>
    </Tooltip>
  )
}
