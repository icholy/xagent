import { createConnectTransport } from "@connectrpc/connect-web";

const TOKEN_KEY = "xagent_token";
const ORG_ID_KEY = "xagent_org_id";

class AuthTransport {
  private refreshPromise: Promise<string> | null = null;
  private listeners: Set<() => void> = new Set();

  getToken(): string | null {
    return localStorage.getItem(TOKEN_KEY);
  }

  getOrgId(): string {
    return localStorage.getItem(ORG_ID_KEY) ?? "0";
  }

  private storeToken(token: string, orgId: string): void {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(ORG_ID_KEY, orgId);
  }

  clearToken(): void {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(ORG_ID_KEY);
  }

  /** Switch to a different org. Clears the token so it gets re-fetched with the new org context. */
  async switchOrg(orgId: string): Promise<void> {
    localStorage.setItem(ORG_ID_KEY, orgId);
    localStorage.removeItem(TOKEN_KEY);
    this.refreshPromise = null;
    await this.fetchToken(orgId);
    this.notifyListeners();
  }

  /** Subscribe to org changes. Returns an unsubscribe function. */
  onOrgChange(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private notifyListeners(): void {
    for (const listener of this.listeners) {
      listener();
    }
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

export const authTransport = new AuthTransport();

// API base URL - uses root path since webui proxies to the backend
export const transport = createConnectTransport({
  baseUrl: "/",
  fetch: authTransport.fetch,
});
