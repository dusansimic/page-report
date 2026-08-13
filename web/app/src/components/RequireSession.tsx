import { Navigate, Outlet } from "react-router";

import { useSession, isSignedIn } from "@/hooks/session";
import { Skeleton } from "@/components/ui";

/** Gate for the signed-in routes. The server enforces this too; this only
 * keeps the user from staring at an empty table while every call 401s. */
export function RequireSession() {
  const { data: session, isPending } = useSession();

  if (isPending) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }
  if (!isSignedIn(session)) {
    return <Navigate to="/" replace />;
  }
  return <Outlet />;
}
