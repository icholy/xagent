import { useEffect } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import {
  getTaskDetails,
  listLogs,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";
import { useNotificationWebSocket } from "@/lib/services";

const invalidationKeys: Record<string, QueryKey[]> = {
  task: [createConnectQueryKey({ schema: getTaskDetails, cardinality: "finite" })],
  log: [createConnectQueryKey({ schema: listLogs, cardinality: "finite" })],
  link: [createConnectQueryKey({ schema: getTaskDetails, cardinality: "finite" })],
  event: [createConnectQueryKey({ schema: getTaskDetails, cardinality: "finite" })],
};

export function useOrgWebSocket() {
  const queryClient = useQueryClient();
  const ws = useNotificationWebSocket();

  useEffect(() => {
    const removeNotification = ws.addNotificationListener((n) => {
      for (const key of invalidationKeys[n.resource] ?? []) {
        queryClient.invalidateQueries({ queryKey: key });
      }
    });
    const removeReconnect = ws.addReconnectListener(() => {
      queryClient.invalidateQueries();
    });
    return () => {
      removeNotification();
      removeReconnect();
    };
  }, [queryClient, ws]);
}
