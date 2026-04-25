import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { createConnectQueryKey } from "@connectrpc/connect-query";
import {
  getTaskDetails,
  listTasks,
  listLogs,
  listEvents,
  getEvent,
  listEventTasks,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";
import { useNotificationWebSocket } from "@/lib/services";
import type { Notification } from "@/lib/notification-websocket";

function invalidateTask(qc: QueryClient) {
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: getTaskDetails, cardinality: "finite" }),
  });
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: listTasks, cardinality: "finite" }),
  });
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: listEventTasks, cardinality: "finite" }),
  });
}

function invalidateLog(qc: QueryClient, taskId: number) {
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({
      schema: listLogs,
      input: { taskId: BigInt(taskId) },
      cardinality: "finite",
    }),
  });
}

function invalidateLink(qc: QueryClient) {
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: getTaskDetails, cardinality: "finite" }),
  });
}

function invalidateEvent(qc: QueryClient) {
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: listEvents, cardinality: "finite" }),
  });
  qc.invalidateQueries({
    queryKey: createConnectQueryKey({ schema: getEvent, cardinality: "finite" }),
  });
}

function handleNotification(qc: QueryClient, n: Notification) {
  switch (n.resource) {
    case "task":
      invalidateTask(qc);
      break;
    case "log":
      invalidateLog(qc, n.id);
      break;
    case "link":
      invalidateLink(qc);
      break;
    case "event":
      invalidateEvent(qc);
      break;
  }
}

export function useOrgWebSocket() {
  const queryClient = useQueryClient();
  const ws = useNotificationWebSocket();

  useEffect(() => {
    const removeNotification = ws.addNotificationListener((n) => {
      handleNotification(queryClient, n);
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
