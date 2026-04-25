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
    if (orgId === NO_ORG) {
      this.setState("idle");
    } else {
      this.connect();
    }
  }

  close() {
    this.closed = true;
    this.disconnect();
    this.setState("idle");
  }

  private disconnect() {
    if (this.es) {
      this.es.close();
      this.es = null;
    }
  }

  private connect() {
    if (this.closed || this.orgId === NO_ORG) return;

    this.setState("connecting");
    this.es = new EventSource(`/events?org_id=${this.orgId}`);

    this.es.addEventListener("ready", () => {
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
      this.events.dispatchEvent(
        new CustomEvent("notification", { detail: n }),
      );
    });

    this.es.onerror = () => {
      this.setState("closed");
      this.events.dispatchEvent(new Event("error"));
    };
  }
}
