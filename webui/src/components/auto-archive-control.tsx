import { Loader2 } from 'lucide-react'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  AUTO_ARCHIVE_IMMEDIATE,
  AUTO_ARCHIVE_NEVER,
  autoArchiveSelectValue,
  durationFromAutoArchiveSelect,
  durationToMillis,
  formatCountdown,
} from '@/lib/duration'
import type { Duration } from '@bufbuild/protobuf/wkt'
import type { Task } from '@/gen/xagent/v1/xagent_pb'

type AutoArchiveTask = Pick<Task, 'status' | 'actions' | 'archived' | 'autoArchive' | 'updatedAt'>

// The presets offered in the dropdown. Positive values are keyed by their
// whole-second count to match autoArchiveSelectValue's lossless encoding.
const PRESETS: { value: string; label: string }[] = [
  { value: AUTO_ARCHIVE_NEVER, label: 'Never' },
  { value: AUTO_ARCHIVE_IMMEDIATE, label: 'Immediately' },
  { value: String(60 * 60), label: '1 hour' },
  { value: String(24 * 60 * 60), label: '24 hours' },
  { value: String(7 * 24 * 60 * 60), label: '7 days' },
]

// AutoArchiveControl shows and edits a task's auto_archive window. It renders the
// current value as a Select. Changing the Select calls onChange with the
// Duration to persist via UpdateTask.
export function AutoArchiveControl({
  task,
  onChange,
  pending,
  disabled,
}: {
  task: AutoArchiveTask
  onChange: (autoArchive: Duration) => void
  pending?: boolean
  disabled?: boolean
}) {
  const current = autoArchiveSelectValue(task.autoArchive)
  // A task's auto_archive can be any duration the API set, not just a preset
  // (e.g. 30m). When it's off-preset, inject an option showing the real value so
  // the trigger renders it faithfully instead of colliding with a preset.
  const isPreset = PRESETS.some((p) => p.value === current)
  const customLabel =
    !isPreset && task.autoArchive ? formatCountdown(durationToMillis(task.autoArchive)) : null
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Auto-archive:</span>
      <Select
        value={current}
        onValueChange={(v) => onChange(durationFromAutoArchiveSelect(v))}
        disabled={disabled || pending}
      >
        <SelectTrigger id="auto-archive" className="h-8 w-auto gap-1 px-3 text-sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {PRESETS.map((p) => (
            <SelectItem key={p.value} value={p.value}>
              {p.label}
            </SelectItem>
          ))}
          {customLabel && <SelectItem value={current}>{customLabel}</SelectItem>}
        </SelectContent>
      </Select>
      {pending && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
    </div>
  )
}
