export interface Notification {
  type: string;
  resource: string;
  id: number;
  org_id: number;
  version: number;
  timestamp: string;
}

export type NotificationListener = (notification: Notification) => void;

/**
 * Manages a WebSocket connection to /ws with automatic reconnection
 * and exponential backoff. Parses incoming JSON notifications and
 * dispatches them to registered listeners.
 */
export class NotificationWebSocket {
  private ws: WebSocket | null = null;
  private closed = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffDelay = 1000;
  private events = new EventTarget();

  constructor() {
    this.connect();
  }

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

  close() {
    this.closed = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  private connect() {
    if (this.closed) return;

    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.ws = new WebSocket(`${protocol}//${location.host}/ws`);

    this.ws.onopen = () => {
      this.backoffDelay = 1000;
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
      if (this.closed) return;
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

export const notificationWebSocket = new NotificationWebSocket();
