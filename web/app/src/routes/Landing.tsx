import { Navigate } from "react-router";

import { useSession, isSignedIn } from "@/hooks/session";
import { Card, LinkButton, Skeleton } from "@/components/ui";

const REPO_URL = "https://github.com/dusansimic/page-report";

// lucide dropped brand marks in v1, and one icon is not worth another
// dependency.
function GithubMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="currentColor" aria-hidden className={className}>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38
        0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01
        1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95
        0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.42 7.42 0 0 1
        2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82
        1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01
        2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

export function Landing() {
  const { data: session, isPending } = useSession();

  if (isPending) {
    return <Skeleton className="h-64 w-full" />;
  }
  if (isSignedIn(session)) {
    return <Navigate to="/dashboard" replace />;
  }

  return (
    <div className="mx-auto max-w-2xl space-y-8">
      <div className="space-y-4">
        <h1 className="text-3xl font-semibold tracking-tight">page-report</h1>
        <p className="text-lg leading-relaxed text-muted">
          A self-hosted place to publish single-page HTML reports and plans.
          Upload one from the command line and share the link &mdash; every
          report is served sandboxed and behind a login.
        </p>
        <p className="leading-relaxed text-muted">
          The project is open source and free to steal: take it, fork it, run
          your own.{" "}
          <a href={REPO_URL} className="text-accent hover:underline">
            {REPO_URL.replace("https://", "")}
          </a>
        </p>
      </div>

      {/*
        A signed-in account that is not on the allowlist would otherwise bounce
        between here and the dashboard forever, so say what happened instead.
      */}
      {session?.authenticated && !session.allowed && (
        <Card className="border-warn/40 p-4 text-sm">
          <p className="font-medium text-warn">Account not allowed</p>
          <p className="mt-1 text-muted">
            You are signed in{session.email ? ` as ${session.email}` : ""}, but
            this account is not on the server&apos;s allowlist. Ask the operator
            to add it, then sign in again.
          </p>
        </Card>
      )}

      <div className="border-t border-line pt-8">
        <LinkButton
          href={`${session?.loginUrl ?? "/auth/login"}?next=%2Fdashboard`}
          size="lg"
        >
          <GithubMark className="size-5" />
          Sign in with GitHub
        </LinkButton>
      </div>
    </div>
  );
}
