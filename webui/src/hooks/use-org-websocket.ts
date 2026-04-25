import { useEffect, useRef } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import {
  getTaskDetails,
  listLogs,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";

interface Notification {
  type: string;
  resource: string;
  id: number;
  org_id: number;
  version: number;
  timestamp: string;
}

const invalidationKeys: Record<string, QueryKey[]> = {
  task: [createConnectQueryKey({ schema: getTaskDetails })],
  log: [createConnectQueryKey({ schema: listLogs })],
  link: [createConnectQueryKey({ schema: getTaskDetails })],
  event: [createConnectQueryKey({ schema: getTaskDetails })],
};

export function useOrgWebSocket() {
  const queryClient = useQueryClient();
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const backoffDelay = useRef(1000);

  useEffect(() => {
    let closed = false;
    let ws: WebSocket | null = null;

    function connect() {
      if (closed) return;

      const protocol = location.protocol === "https:" ? "wss:" : "ws:";
      ws = new WebSocket(`${protocol}//${location.host}/ws`);

      ws.onopen = () => {
        backoffDelay.current = 1000;
        // Catch up on anything missed while disconnected
        queryClient.invalidateQueries();
      };

      ws.onmessage = (event) => {
        let n: Notification;
        try {
          n = JSON.parse(event.data);
        } catch {
          console.warn("useOrgWebSocket: failed to parse message", event.data);
          return;
        }
        const keys = invalidationKeys[n.resource];
        if (keys) {
          for (const key of keys) {
            queryClient.invalidateQueries({ queryKey: key });
          }
        }
      };

      ws.onclose = () => {
        if (closed) return;
        const delay =
          backoffDelay.current + Math.random() * 1000;
        backoffDelay.current = Math.min(backoffDelay.current * 2, 30000);
        reconnectTimer.current = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        // onclose will fire after onerror, triggering reconnect
      };
    }

    connect();

    return () => {
      closed = true;
      if (reconnectTimer.current !== null) {
        clearTimeout(reconnectTimer.current);
        reconnectTimer.current = null;
      }
      if (ws) {
        ws.close();
      }
    };
  }, [queryClient]);
}
