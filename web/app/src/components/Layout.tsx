import { NavLink, Outlet, Link } from "react-router";
import { FileText, KeyRound } from "lucide-react";

import { useSession, isSignedIn } from "@/hooks/session";
import { cn } from "@/lib/utils";

const REPO_URL = "https://github.com/dusansimic/page-report";

export function Layout() {
  const { data: session } = useSession();
  const signedIn = isSignedIn(session);

  return (
    <div className="min-h-dvh">
      <header className="border-b border-line">
        <div className="mx-auto flex max-w-5xl items-center gap-6 px-6 py-3">
          <Link to="/" className="font-semibold tracking-tight">
            page-report
          </Link>

          {signedIn && (
            <nav className="flex items-center gap-1 text-sm">
              <NavItem to="/dashboard" icon={<FileText className="size-4" />}>
                Reports
              </NavItem>
              <NavItem to="/tokens" icon={<KeyRound className="size-4" />}>
                Tokens
              </NavItem>
            </nav>
          )}

          <div className="ml-auto flex items-center gap-3 text-sm">
            {signedIn && <Account session={session!} />}
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-6 py-10">
        <Outlet />
      </main>

      <footer className="mx-auto max-w-5xl px-6 pb-10 text-xs text-muted">
        <a href={REPO_URL} className="hover:text-fg">
          Source on GitHub
        </a>
      </footer>
    </div>
  );
}

function NavItem({
  to,
  icon,
  children,
}: {
  to: string;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        cn(
          "flex items-center gap-2 rounded-md px-3 py-1.5 transition-colors",
          isActive ? "bg-panel text-fg" : "text-muted hover:text-fg",
        )
      }
    >
      {icon}
      {children}
    </NavLink>
  );
}

function Account({
  session,
}: {
  session: { email: string; login: string; avatarUrl: string; logoutUrl: string };
}) {
  const name = session.email || session.login;
  return (
    <div className="flex items-center gap-3">
      {session.avatarUrl && (
        <img
          src={session.avatarUrl}
          alt=""
          width={24}
          height={24}
          className="rounded-full"
        />
      )}
      <span className="text-muted">{name}</span>
      {/*
        A real form post, not a fetch: signing out clears a cookie and should
        leave the app in a freshly loaded state rather than a stale cache.
      */}
      <form method="post" action={session.logoutUrl}>
        <input type="hidden" name="next" value="/" />
        <button
          type="submit"
          className="cursor-pointer text-muted hover:text-fg"
        >
          Sign out
        </button>
      </form>
    </div>
  );
}
