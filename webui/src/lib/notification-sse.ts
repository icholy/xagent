import { NO_ORG } from "./transport";

export interface NotificationResource {
  action: string;
  type: string;
  id: number;
}

export interface Notification {
  type: "ready" | "change";
  resources?: NotificationResource[];
  org_id: number;
  user_id?: string;
  client_id?: string;
  timestamp: string;
}

export type NotificationListener = (notification: Notification) => void;
export type ConnectionState = "idle" | "connecting" | "open" | "closed";

export class NotificationSSE {
  private es: EventSource | null = null;
  private closed = false;
  private events = new EventTarget();
  private orgId: string = NO_ORG;
  private state: ConnectionState = "idle";
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempts = 0;
  private clientId: string;

  constructor(clientId: string) {
    this.clientId = clientId;
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", this.handleVisibilityChange);
    }
  }

  private handleVisibilityChange = () => {
    if (this.closed || this.orgId === NO_ORG) return;
    if (document.visibilityState === "visible") {
      // Coming back to the foreground: reconnect immediately.
      this.reconnectAttempts = 0;
      this.connect();
    } else {
      // Backgrounded: proactively close. The native EventSource often gets
      // stuck in a half-dead state on mobile when the tab is hidden, and the
      // browser's built-in reconnect doesn't recover. Closing here means we
      // always re-establish a fresh connection on visibilitychange → visible.
      this.disconnect();
      this.setState("idle");
    }
  };

  addNotificationListener(listener: NotificationListener): () => void {
    const handler = (e: Event) => {
      listener((e as CustomEvent<Notification>).detail);
    };
    this.events.addEventListener("notification", handler);
    return () => this.events.removeEventListener("notification", handler);
  }

  addReconnectListener(listener: () => void): () => void {
    this.events.addEventListener("reconnect", listener);
    return () => this.events.removeEventListener("reconnect", listener);
  }

  addErrorListener(listener: () => void): () => void {
    this.events.addEventListener("error", listener);
    return () => this.events.removeEventListener("error", listener);
  }

  getState(): ConnectionState {
    return this.state;
  }

  addStateListener(listener: (state: ConnectionState) => void): () => void {
    const handler = (e: Event) => {
      listener((e as CustomEvent<ConnectionState>).detail);
    };
    this.events.addEventListener("state", handler);
    return () => this.events.removeEventListener("state", handler);
  }

  private setState(next: ConnectionState) {
    if (next === this.state) return;
    this.state = next;
    this.events.dispatchEvent(new CustomEvent("state", { detail: next }));
  }

  setOrgId(orgId: string) {
    if (orgId === this.orgId) return;
    this.orgId = orgId;
    this.disconnect();
    this.reconnectAttempts = 0;
    if (orgId === NO_ORG) {
      this.setState("idle");
    } else {
      this.connect();
    }
  }

  close() {
    this.closed = true;
    this.disconnect();
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", this.handleVisibilityChange);
    }
    this.setState("idle");
  }

  private disconnect() {
    if (this.es) {
      this.es.close();
      this.es = null;
    }
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private scheduleReconnect() {
    if (this.closed || this.orgId === NO_ORG) return;
    if (this.reconnectTimer !== null) return;
    const delay = Math.min(1000 * 2 ** this.reconnectAttempts, 30_000);
    this.reconnectAttempts += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private connect() {
    if (this.closed || this.orgId === NO_ORG) return;
    this.disconnect();

    this.setState("connecting");
    this.es = new EventSource(`/events?org_id=${this.orgId}`);

    this.es.addEventListener("ready", () => {
      this.reconnectAttempts = 0;
      this.setState("open");
      this.events.dispatchEvent(new Event("reconnect"));
    });

    this.es.addEventListener("change", (event) => {
      let n: Notification;
      try {
        n = JSON.parse(event.data);
      } catch {
        console.warn("NotificationSSE: failed to parse message", event.data);
        return;
      }
      // Skip notifications originating from this tab
      if (n.client_id === this.clientId) return;
      this.events.dispatchEvent(
        new CustomEvent("notification", { detail: n }),
      );
    });

    this.es.onerror = () => {
      this.setState("closed");
      this.events.dispatchEvent(new Event("error"));
      // Browser EventSource auto-reconnect is unreliable on mobile (especially
      // after the tab is backgrounded). Tear down and reconnect ourselves.
      if (this.es) {
        this.es.close();
        this.es = null;
      }
      this.scheduleReconnect();
    };
  }
}
