import { NO_ORG } from "./transport";

export interface Notification {
  type: string;
  resource: string;
  id: number;
  org_id: number;
  version: number;
  timestamp: string;
}

export type NotificationListener = (notification: Notification) => void;

// idle: no org set
// connecting: opening or waiting on a reconnect attempt's onopen
// open: connected to /ws
// closed: connection lost; waiting on the backoff timer to retry
export type ConnectionState = "idle" | "connecting" | "open" | "closed";

/**
 * Manages a WebSocket connection to /ws with automatic reconnection
 * and exponential backoff. Parses incoming JSON notifications and
 * dispatches them to registered listeners.
 *
 * Idle by default — call setOrgId() with a real org id to open a subscription.
 */
export class NotificationWebSocket {
  private ws: WebSocket | null = null;
  private closed = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffDelay = 1000;
  private events = new EventTarget();
  // Active org for the subscription. NO_ORG means "do not connect";
  // any other value is the org id sent to /ws as ?org_id=.
  private orgId: string = NO_ORG;
  private state: ConnectionState = "idle";

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

  // Opens a subscription for the given org, replacing any existing
  // connection. Pass NO_ORG to disconnect. No-op when the orgId
  // hasn't changed.
  setOrgId(orgId: string) {
    if (orgId === this.orgId) return;
    this.orgId = orgId;
    this.disconnect();
    if (orgId === NO_ORG) {
      this.setState("idle");
    } else {
      this.backoffDelay = 1000;
      this.connect();
    }
  }

  close() {
    this.closed = true;
    this.disconnect();
    this.setState("idle");
  }

  private disconnect() {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
  }

  private connect() {
    if (this.closed || this.orgId === NO_ORG) return;

    this.setState("connecting");
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.ws = new WebSocket(`${protocol}//${location.host}/ws?org_id=${this.orgId}`);

    this.ws.onopen = () => {
      this.backoffDelay = 1000;
      this.setState("open");
      this.events.dispatchEvent(new Event("reconnect"));
    };

    this.ws.onmessage = (event) => {
      let n: Notification;
      try {
        n = JSON.parse(event.data);
      } catch {
        console.warn(
          "NotificationWebSocket: failed to parse message",
          event.data,
        );
        return;
      }
      this.events.dispatchEvent(
        new CustomEvent("notification", { detail: n }),
      );
    };

    this.ws.onclose = () => {
      if (this.closed || this.orgId === NO_ORG) return;
      this.setState("closed");
      const delay = this.backoffDelay + Math.random() * 1000;
      this.backoffDelay = Math.min(this.backoffDelay * 2, 30000);
      this.reconnectTimer = setTimeout(() => this.connect(), delay);
    };

    this.ws.onerror = () => {
      console.warn("NotificationWebSocket: connection error, reconnecting...");
      this.events.dispatchEvent(new Event("error"));
      // onclose fires after onerror, triggering reconnect
    };
  }
}
