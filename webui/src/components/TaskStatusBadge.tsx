import { Badge } from "@/components/ui/badge";
import type { TaskStatus } from "@/lib/api";

const statusStyles: Record<TaskStatus, string> = {
  pending: "bg-amber-100 text-amber-800 hover:bg-amber-100",
  running: "bg-blue-100 text-blue-800 hover:bg-blue-100",
  completed: "bg-green-100 text-green-800 hover:bg-green-100",
  failed: "bg-red-100 text-red-800 hover:bg-red-100",
  cancelled: "bg-amber-100 text-amber-800 hover:bg-amber-100",
  restarting: "bg-pink-100 text-pink-800 hover:bg-pink-100",
  archived: "bg-gray-100 text-gray-600 hover:bg-gray-100",
};

interface TaskStatusBadgeProps {
  status: TaskStatus;
}

export function TaskStatusBadge({ status }: TaskStatusBadgeProps) {
  return (
    <Badge variant="secondary" className={statusStyles[status]}>
      {status}
    </Badge>
  );
}
