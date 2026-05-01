import { formatDistanceToNow } from 'date-fns'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

export function RelativeTime({ date }: { date: Date }) {

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
        {formatDistanceToNow(date, { addSuffix: true })}
      </TooltipTrigger>
      <TooltipContent>
        {absoluteText}
      </TooltipContent>
    </Tooltip>
  )
}
