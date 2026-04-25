import { useConnectionState } from "@/hooks/use-connection-state";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const styles = {
  idle: { color: "bg-muted-foreground/40", label: "Disconnected", pulse: false },
  connecting: { color: "bg-yellow-500", label: "Connecting…", pulse: true },
  open: { color: "bg-green-500", label: "Connected", pulse: false },
  closed: { color: "bg-red-500", label: "Reconnecting…", pulse: true },
} as const;

export function ConnectionIndicator() {
  const state = useConnectionState();
  const { color, label, pulse } = styles[state];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          aria-label={label}
          className={cn("inline-block h-2 w-2 rounded-full", color, pulse && "animate-pulse")}
        />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
