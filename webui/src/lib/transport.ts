const TOKEN_KEY = "xagent_token";
const ORG_ID_KEY = "xagent_org_id";

export class AuthTransport {
  private refreshPromise: Promise<string> | null = null;
  private events = new EventTarget();
  private lastOrgId: string;

  constructor() {
    this.lastOrgId = this.getOrgId();
  }

  onOrgChange(listener: () => void): () => void {
    this.events.addEventListener("orgchange", listener);
    return () => this.events.removeEventListener("orgchange", listener);
  }

  getToken(): string | null {
    return localStorage.getItem(TOKEN_KEY);
  }

  getOrgId(): string {
    return localStorage.getItem(ORG_ID_KEY) ?? "0";
  }

  private notifyOrgChange(): void {
    const next = this.getOrgId();
    if (next === this.lastOrgId) return;
    this.lastOrgId = next;
    this.events.dispatchEvent(new Event("orgchange"));
  }

  private storeToken(token: string, orgId: string): void {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(ORG_ID_KEY, orgId);
    this.notifyOrgChange();
  }

  clearToken(): void {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(ORG_ID_KEY);
    this.notifyOrgChange();
  }

  async fetchToken(orgId?: string): Promise<string> {
    const id = orgId ?? this.getOrgId();
    const resp = await fetch(`/auth/token?org_id=${encodeURIComponent(id)}`);
    if (resp.status === 401) {
      this.clearToken();
      window.location.href = "/auth/login";
      throw new Error("session expired");
    }
    if (!resp.ok) {
      throw new Error(`failed to fetch token: ${resp.status}`);
    }
    const data = await resp.json();
    this.storeToken(data.token, String(data.org_id));
    return data.token;
  }

  private async refreshToken(): Promise<string> {
    if (!this.refreshPromise) {
      this.refreshPromise = this.fetchToken().finally(() => {
        this.refreshPromise = null;
      });
    }
    return this.refreshPromise;
  }

  fetch: typeof globalThis.fetch = async (input, init) => {
    let token = this.getToken();
    if (!token) {
      token = await this.refreshToken();
    }

    const headers = new Headers(init?.headers);
    headers.set("Authorization", `Bearer ${token}`);
    headers.set("X-Auth-Type", "app");

    let resp = await fetch(input, { ...init, headers });

    // On 401, try refreshing the token once
    if (resp.status === 401) {
      try {
        token = await this.refreshToken();
      } catch {
        return resp;
      }
      headers.set("Authorization", `Bearer ${token}`);
      resp = await fetch(input, { ...init, headers });
    }

    return resp;
  };
}

