import { createConnectTransport } from "@connectrpc/connect-web";

const TOKEN_KEY = "xagent_token";
const ORG_ID_KEY = "xagent_org_id";

function getStoredToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

function getStoredOrgId(): string {
  return localStorage.getItem(ORG_ID_KEY) ?? "0";
}

function storeToken(token: string, orgId: string): void {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(ORG_ID_KEY, orgId);
}

function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(ORG_ID_KEY);
}

async function fetchToken(orgId?: string): Promise<string> {
  const id = orgId ?? getStoredOrgId();
  const resp = await fetch(`/auth/token?org_id=${encodeURIComponent(id)}`);
  if (resp.status === 401) {
    clearToken();
    window.location.href = "/auth/login";
    throw new Error("session expired");
  }
  if (!resp.ok) {
    throw new Error(`failed to fetch token: ${resp.status}`);
  }
  const data = await resp.json();
  storeToken(data.token, String(data.org_id));
  return data.token;
}

// Ensure only one token refresh happens at a time
let refreshPromise: Promise<string> | null = null;

async function refreshToken(): Promise<string> {
  if (!refreshPromise) {
    refreshPromise = fetchToken().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

// Custom fetch that attaches the app JWT and handles 401 refresh
const authenticatedFetch: typeof globalThis.fetch = async (input, init) => {
  let token = getStoredToken();
  if (!token) {
    token = await refreshToken();
  }

  const headers = new Headers(init?.headers);
  headers.set("Authorization", `Bearer ${token}`);
  headers.set("X-Auth-Type", "app");

  let resp = await fetch(input, { ...init, headers });

  // On 401, try refreshing the token once
  if (resp.status === 401) {
    try {
      token = await refreshToken();
    } catch {
      return resp;
    }
    headers.set("Authorization", `Bearer ${token}`);
    resp = await fetch(input, { ...init, headers });
  }

  return resp;
};

// API base URL - uses root path since webui proxies to the backend
export const transport = createConnectTransport({
  baseUrl: "/",
  fetch: authenticatedFetch,
});

// Re-export for org switching
export { fetchToken, getStoredOrgId, clearToken };
