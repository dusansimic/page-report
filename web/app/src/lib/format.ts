import { formatDistanceToNow, format } from "date-fns";

/** Protobuf carries these as Unix seconds; 0 means "unset". */
export function toDate(unixSeconds: bigint | number): Date | null {
  const n = Number(unixSeconds);
  return n > 0 ? new Date(n * 1000) : null;
}

export function relative(unixSeconds: bigint | number, fallback = "never"): string {
  const d = toDate(unixSeconds);
  return d ? `${formatDistanceToNow(d)} ago` : fallback;
}

export function absolute(unixSeconds: bigint | number, fallback = "—"): string {
  const d = toDate(unixSeconds);
  return d ? format(d, "yyyy-MM-dd HH:mm") : fallback;
}

export function bytes(n: bigint | number): string {
  const size = Number(n);
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
