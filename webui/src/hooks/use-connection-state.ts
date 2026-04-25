import { useSyncExternalStore } from "react";
import { useNotificationWebSocket } from "@/lib/services";
import type { ConnectionState } from "@/lib/notification-websocket";

/** Subscribes to the WebSocket connection state, re-rendering on change. */
export function useConnectionState(): ConnectionState {
  const ws = useNotificationWebSocket();
  return useSyncExternalStore(
    (cb) => ws.addStateListener(cb),
    () => ws.getState(),
  );
}
