import { Duration } from '@icholy/duration'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

export function RelativeTime({ date }: { date: Date }) {
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  const duration = new Duration(diff)

  const relativeText = diff < 1000 ? 'just now' : `${duration.toString()} ago`
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
