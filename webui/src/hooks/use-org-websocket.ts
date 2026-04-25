import { useEffect } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import {
  getTaskDetails,
  listLogs,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";
import { NotificationWebSocket } from "@/lib/notification-websocket";

const invalidationKeys: Record<string, QueryKey[]> = {
  task: [createConnectQueryKey({ schema: getTaskDetails })],
  log: [createConnectQueryKey({ schema: listLogs })],
  link: [createConnectQueryKey({ schema: getTaskDetails })],
  event: [createConnectQueryKey({ schema: getTaskDetails })],
};

export function useOrgWebSocket() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const ws = new NotificationWebSocket({
      onNotification: (n) => {
        const keys = invalidationKeys[n.resource];
        if (keys) {
          for (const key of keys) {
            queryClient.invalidateQueries({ queryKey: key });
          }
        }
      },
      onReconnect: () => {
        queryClient.invalidateQueries();
      },
    });
    return () => ws.close();
  }, [queryClient]);
}
