export interface Notification {
  type: string;
  resource: string;
  id: number;
  org_id: number;
  version: number;
  timestamp: string;
}

export type NotificationHandler = (notification: Notification) => void;

/**
 * Manages a WebSocket connection to /ws with automatic reconnection
 * and exponential backoff. Parses incoming JSON notifications and
 * dispatches them to registered handlers.
 */
export class NotificationWebSocket {
  private ws: WebSocket | null = null;
  private closed = false;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffDelay = 1000;
  private onNotification: NotificationHandler;
  private onReconnect: () => void;

  constructor(opts: {
    onNotification: NotificationHandler;
    onReconnect: () => void;
  }) {
    this.onNotification = opts.onNotification;
    this.onReconnect = opts.onReconnect;
    this.connect();
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
      this.onReconnect();
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
      this.onNotification(n);
    };

    this.ws.onclose = () => {
      if (this.closed) return;
      const delay = this.backoffDelay + Math.random() * 1000;
      this.backoffDelay = Math.min(this.backoffDelay * 2, 30000);
      this.reconnectTimer = setTimeout(() => this.connect(), delay);
    };

    this.ws.onerror = () => {
      // onclose fires after onerror, triggering reconnect
    };
  }
}
