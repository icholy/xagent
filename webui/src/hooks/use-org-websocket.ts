import { useEffect } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import {
  getTaskDetails,
  listLogs,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";
import { notificationWebSocket } from "@/lib/notification-websocket";

const invalidationKeys: Record<string, QueryKey[]> = {
  task: [createConnectQueryKey({ schema: getTaskDetails })],
  log: [createConnectQueryKey({ schema: listLogs })],
  link: [createConnectQueryKey({ schema: getTaskDetails })],
  event: [createConnectQueryKey({ schema: getTaskDetails })],
};

export function useOrgWebSocket() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const onNotification = (e: Event) => {
      const { notification: n } = e as { notification: { resource: string } };
      for (const key of invalidationKeys[n.resource] ?? []) {
        queryClient.invalidateQueries({ queryKey: key });
      }
    };
    const onReconnect = () => {
      queryClient.invalidateQueries();
    };
    notificationWebSocket.addEventListener("notification", onNotification);
    notificationWebSocket.addEventListener("reconnect", onReconnect);
    return () => {
      notificationWebSocket.removeEventListener(
        "notification",
        onNotification,
      );
      notificationWebSocket.removeEventListener("reconnect", onReconnect);
    };
  }, [queryClient]);
}
