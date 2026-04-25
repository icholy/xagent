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
  listWorkspaces,
  listOrgMembers,
  listKeys,
} from "@/gen/xagent/v1/xagent-XAgentService_connectquery";
import { useNotificationWebSocket } from "@/lib/services";
import type { Notification, NotificationResource } from "@/lib/notification-websocket";

function invalidateResource(qc: QueryClient, r: NotificationResource) {
  console.debug("[ws] invalidate", r);
  switch (r.type) {
    case "task":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getTaskDetails,
          input: { id: BigInt(r.id) },
          cardinality: "finite",
        }),
      });
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listTasks, cardinality: "finite" }),
      });
      break;
    case "task_logs":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: listLogs,
          input: { taskId: BigInt(r.id) },
          cardinality: "finite",
        }),
      });
      break;
    case "task_links":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getTaskDetails,
          input: { id: BigInt(r.id) },
          cardinality: "finite",
        }),
      });
      break;
    case "event":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listEvents, cardinality: "finite" }),
      });
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getEvent,
          input: { id: BigInt(r.id) },
          cardinality: "finite",
        }),
      });
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: listEventTasks,
          input: { eventId: BigInt(r.id) },
          cardinality: "finite",
        }),
      });
      break;
    case "workspaces":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listWorkspaces, cardinality: "finite" }),
      });
      break;
    case "org_members":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listOrgMembers, cardinality: "finite" }),
      });
      break;
    case "keys":
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listKeys, cardinality: "finite" }),
      });
      break;
    default:
      console.warn("[ws] unhandled resource type", r);
  }
}

function handleNotification(qc: QueryClient, n: Notification) {
  console.debug("[ws] notification", n);
  if (n.type === "ready") {
    return;
  }
  for (const r of n.resources ?? []) {
    invalidateResource(qc, r);
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
