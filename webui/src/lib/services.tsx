import { createContext, useContext, type ReactNode } from "react";
import type { AuthTransport } from "./transport";
import type { NotificationWebSocket } from "./notification-websocket";

export interface Services {
  auth: AuthTransport;
  ws: NotificationWebSocket;
}

const ServicesContext = createContext<Services | null>(null);

export function ServicesProvider({
  services,
  children,
}: {
  services: Services;
  children: ReactNode;
}) {
  return (
    <ServicesContext.Provider value={services}>
      {children}
    </ServicesContext.Provider>
  );
}

export function useAuthTransport(): AuthTransport {
  const s = useContext(ServicesContext);
  if (!s) {
    throw new Error("useAuthTransport must be used within ServicesProvider");
  }
  return s.auth;
}

export function useNotificationWebSocket(): NotificationWebSocket {
  const s = useContext(ServicesContext);
  if (!s) {
    throw new Error("useNotificationWebSocket must be used within ServicesProvider");
  }
  return s.ws;
}
