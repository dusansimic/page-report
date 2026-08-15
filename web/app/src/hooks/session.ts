import { useQuery } from "@tanstack/react-query";
import { createClient } from "@connectrpc/connect";

import { DashboardService } from "@/gen/pagereport/v1/dashboard_pb";
import type { GetSessionResponse } from "@/gen/pagereport/v1/dashboard_pb";
import { transport } from "@/lib/transport";

export const dashboardClient = createClient(DashboardService, transport);

export const sessionQueryKey = ["session"] as const;

/**
 * GetSession is safe to call while logged out — it answers with
 * `authenticated: false` rather than an error — so every screen can start from
 * it without a separate "are we logged in" path.
 */
export function useSession() {
  return useQuery<GetSessionResponse>({
    queryKey: sessionQueryKey,
    queryFn: () => dashboardClient.getSession({}),
    staleTime: 30_000,
  });
}

/** True once the session is loaded and allowed to use the dashboard. */
export function isSignedIn(session: GetSessionResponse | undefined): boolean {
  return Boolean(session?.authenticated && session.allowed);
}
