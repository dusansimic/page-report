import { createConnectTransport } from "@connectrpc/connect-web";
import { ConnectError, Code } from "@connectrpc/connect";

/**
 * The API is served from the same origin as this app, so requests are
 * same-origin and the session cookie rides along automatically. The server
 * additionally requires an Origin header matching its configured base URL, and
 * registers no CORS handler, so a copy of this app hosted elsewhere cannot talk
 * to it.
 */
export const transport = createConnectTransport({
  baseUrl: "/",
  // The server speaks Connect with JSON on the wire.
  useBinaryFormat: false,
  fetch: (input, init) => fetch(input, { ...init, credentials: "same-origin" }),
});

/** Sentinel the server returns when a token mint needs a fresher login. */
const REAUTH_REQUIRED = "reauth_required";

export function isReauthRequired(err: unknown): boolean {
  return (
    err instanceof ConnectError &&
    err.code === Code.PermissionDenied &&
    err.rawMessage.includes(REAUTH_REQUIRED)
  );
}

export function isUnauthenticated(err: unknown): boolean {
  return err instanceof ConnectError && err.code === Code.Unauthenticated;
}

export function errorMessage(err: unknown): string {
  if (err instanceof ConnectError) return err.rawMessage;
  return err instanceof Error ? err.message : String(err);
}

/**
 * Sends the browser through the identity provider and back to `next`. This has
 * to be a full navigation: an OAuth redirect cannot be followed by fetch().
 */
export function login(next: string, loginUrl = "/auth/login"): void {
  window.location.href = `${loginUrl}?next=${encodeURIComponent(next)}`;
}
