import { NavLink, Outlet, Link } from "react-router";
import { FileText, KeyRound } from "lucide-react";

import { useSession, isSignedIn } from "@/hooks/session";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const REPO_URL = "https://github.com/dusansimic/page-report";

export function Layout() {
  const { data: session } = useSession();
  const signedIn = isSignedIn(session);

  return (
    <div className="min-h-dvh">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl items-center gap-6 px-6 py-3">
          <Link to="/" className="font-heading text-sm font-semibold tracking-tight">
            page-report
          </Link>

          {signedIn && (
            <nav className="flex items-center gap-1">
              <NavItem to="/dashboard" icon={<FileText />}>
                Reports
              </NavItem>
              <NavItem to="/tokens" icon={<KeyRound />}>
                Tokens
              </NavItem>
            </nav>
          )}

          <div className="ml-auto flex items-center gap-3 text-xs/relaxed">
            {signedIn && <Account session={session!} />}
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-6 py-10">
        <Outlet />
      </main>

      <footer className="mx-auto max-w-5xl px-6 pb-10 text-[0.625rem] text-muted-foreground">
        <a href={REPO_URL} className="hover:text-foreground">
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
          buttonVariants({ variant: "ghost", size: "sm" }),
          isActive
            ? "bg-muted text-foreground"
            : "text-muted-foreground",
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
          width={22}
          height={22}
          className="rounded-full"
        />
      )}
      <span className="text-muted-foreground">{name}</span>
      {/*
        A real form post, not a fetch: signing out clears a cookie and should
        leave the app in a freshly loaded state rather than a stale cache.
      */}
      <form method="post" action={session.logoutUrl}>
        <input type="hidden" name="next" value="/" />
        <button
          type="submit"
          className="cursor-pointer text-muted-foreground hover:text-foreground"
        >
          Sign out
        </button>
      </form>
    </div>
  );
}
