import { Loader2, MoreHorizontal } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  AUTO_ARCHIVE_IMMEDIATE,
  AUTO_ARCHIVE_NEVER,
  autoArchiveSelectValue,
  durationFromAutoArchiveSelect,
  durationToMillis,
  formatCountdown,
} from '@/lib/duration'
import { canArchiveTask, canUnarchiveTask, isArchivedTask } from '@/lib/task'
import type { Duration } from '@bufbuild/protobuf/wkt'
import type { Task } from '@/gen/xagent/v1/xagent_pb'

type ActionsTask = Pick<Task, 'status' | 'actions' | 'archived' | 'autoArchive' | 'updatedAt'>

// The auto-archive presets offered in the submenu. Positive values are keyed by
// their whole-second count to match autoArchiveSelectValue's lossless encoding.
const AUTO_ARCHIVE_PRESETS: { value: string; label: string }[] = [
  { value: AUTO_ARCHIVE_NEVER, label: 'Never' },
  { value: AUTO_ARCHIVE_IMMEDIATE, label: 'Immediately' },
  { value: String(60 * 60), label: '1 hour' },
  { value: String(6 * 60 * 60), label: '6 hours' },
  { value: String(24 * 60 * 60), label: '24 hours' },
  { value: String(7 * 24 * 60 * 60), label: '7 days' },
]

// TaskActionsMenu is the overflow (…) menu in the task page header. It is the
// single entry point for the task's auto-archive delay (a duration submenu) and
// its archive/unarchive action (shown according to the current archive state).
export function TaskActionsMenu({
  task,
  onAutoArchiveChange,
  autoArchivePending,
  onArchive,
  archivePending,
  onUnarchive,
  unarchivePending,
}: {
  task: ActionsTask
  onAutoArchiveChange: (autoArchive: Duration) => void
  autoArchivePending?: boolean
  onArchive: () => void
  archivePending?: boolean
  onUnarchive: () => void
  unarchivePending?: boolean
}) {
  const current = autoArchiveSelectValue(task.autoArchive)
  const preset = AUTO_ARCHIVE_PRESETS.find((p) => p.value === current)
  // A task's auto_archive can be any duration the API set, not just a preset
  // (e.g. 30m). When it's off-preset, show its real value on the trigger and add
  // a matching row so the current selection is still reflected.
  const customLabel =
    !preset && task.autoArchive ? formatCountdown(durationToMillis(task.autoArchive)) : null
  const currentLabel = preset?.label ?? customLabel ?? 'Never'
  const pending = autoArchivePending || archivePending || unarchivePending

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Task actions" disabled={pending}>
          {pending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <MoreHorizontal className="h-4 w-4" />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuSub>
          {/* Disabled once archived: an archived task no longer auto-archives. */}
          <DropdownMenuSubTrigger disabled={isArchivedTask(task)}>
            Auto-archive
            <span className="ml-auto text-muted-foreground">{currentLabel}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuPortal>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={current}
                onValueChange={(v) => onAutoArchiveChange(durationFromAutoArchiveSelect(v))}
              >
                {AUTO_ARCHIVE_PRESETS.map((p) => (
                  <DropdownMenuRadioItem key={p.value} value={p.value}>
                    {p.label}
                  </DropdownMenuRadioItem>
                ))}
                {customLabel && (
                  <DropdownMenuRadioItem value={current}>{customLabel}</DropdownMenuRadioItem>
                )}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuPortal>
        </DropdownMenuSub>

        {/* Archive-state-aware: the server exposes exactly one of these actions. */}
        {canUnarchiveTask(task) && (
          <DropdownMenuItem onSelect={onUnarchive}>Unarchive task</DropdownMenuItem>
        )}
        {canArchiveTask(task) && (
          <DropdownMenuItem onSelect={onArchive}>Archive task</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
