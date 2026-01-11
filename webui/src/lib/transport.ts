import { createConnectTransport } from "@connectrpc/connect-web";

// API base URL - uses root path since webui proxies to the backend
export const transport = createConnectTransport({
  baseUrl: "/",
});
