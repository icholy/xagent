import { useSyncExternalStore } from "react";
import { useNotificationSSE } from "@/lib/services";
import type { ConnectionState } from "@/lib/notification-sse";

/** Subscribes to the SSE connection state, re-rendering on change. */
export function useConnectionState(): ConnectionState {
  const sse = useNotificationSSE();
  return useSyncExternalStore(
    (cb) => sse.addStateListener(cb),
    () => sse.getState(),
  );
}
