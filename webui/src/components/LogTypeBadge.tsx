import { Badge } from "@/components/ui/badge";

const typeStyles: Record<string, string> = {
  llm: "bg-violet-100 text-violet-800 hover:bg-violet-100",
  info: "bg-blue-100 text-blue-800 hover:bg-blue-100",
  error: "bg-red-100 text-red-800 hover:bg-red-100",
};

interface LogTypeBadgeProps {
  type: string;
}

export function LogTypeBadge({ type }: LogTypeBadgeProps) {
  return (
    <Badge variant="secondary" className={typeStyles[type] ?? "bg-gray-100 text-gray-600 hover:bg-gray-100"}>
      {type}
    </Badge>
  );
}
