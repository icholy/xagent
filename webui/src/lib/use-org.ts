import { useCallback, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { authTransport } from "./transport";

/** Returns the current org ID as a bigint, suitable for passing to RPC requests. */
export function useOrgId(): bigint {
  const orgIdStr = useSyncExternalStore(
    (cb) => authTransport.onOrgChange(cb),
    () => authTransport.getOrgId()
  );
  return BigInt(orgIdStr);
}

/** Returns a callback that switches to a different org and invalidates all queries. */
export function useSwitchOrg(): (orgId: bigint) => Promise<void> {
  const queryClient = useQueryClient();
  return useCallback(
    async (orgId: bigint) => {
      await authTransport.switchOrg(String(orgId));
      queryClient.invalidateQueries();
    },
    [queryClient]
  );
}
