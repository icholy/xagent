import { Badge } from '@/components/ui/badge'

const commandStyles: Record<string, string> = {
  restart: 'bg-pink-100 text-pink-800 border-pink-200',
  stop: 'bg-orange-100 text-orange-800 border-orange-200',
  start: 'bg-green-100 text-green-800 border-green-200',
}

export function CommandBadge({ command }: { command: string }) {
  return (
    <Badge
      variant="outline"
      className={commandStyles[command] ?? 'bg-gray-100 text-gray-600'}
    >
      command:{command}
    </Badge>
  )
}
