import type { PageMeta } from "@/gen/pagereport/v1/pagereport_pb";

export type PageEntry = { kind: "page"; page: PageMeta };
export type GroupEntry = {
  kind: "group";
  /** `${source}:${id}` */
  key: string;
  source: string;
  /** Newest non-empty session title; "" when none (UI supplies a fallback). */
  title: string;
  /** Newest first. */
  pages: PageMeta[];
  latestAt: bigint;
  totalBytes: bigint;
};
export type Entry = PageEntry | GroupEntry;

const sourceLabels: Record<string, string> = {
  t3code: "T3 Code",
  "claude-code": "Claude Code",
  codex: "Codex",
  opencode: "OpenCode",
  manual: "Manual",
};

/** Human label for a session source; unknown sources show as-is. */
export function sourceLabel(source: string): string {
  return sourceLabels[source] ?? source;
}

const newestFirst = (a: PageMeta, b: PageMeta) =>
  a.createdAt === b.createdAt ? 0 : a.createdAt > b.createdAt ? -1 : 1;

/**
 * Folds pages sharing a session into groups. Pages without a session id, and
 * sessions with a single page, stay plain entries. Entries are ordered by
 * newest page first.
 */
export function groupPages(pages: PageMeta[]): Entry[] {
  const buckets = new Map<string, PageMeta[]>();
  const entries: Entry[] = [];
  for (const page of [...pages].sort(newestFirst)) {
    const s = page.session;
    if (!s || !s.id) {
      entries.push({ kind: "page", page });
      continue;
    }
    const key = `${s.source}:${s.id}`;
    const bucket = buckets.get(key);
    if (bucket) bucket.push(page);
    else buckets.set(key, [page]);
  }
  for (const [key, group] of buckets) {
    if (group.length === 1) {
      entries.push({ kind: "page", page: group[0] });
      continue;
    }
    entries.push({
      kind: "group",
      key,
      source: group[0].session?.source ?? "",
      title: group.find((p) => p.session?.title)?.session?.title ?? "",
      pages: group,
      latestAt: group[0].createdAt,
      totalBytes: group.reduce((n, p) => n + p.sizeBytes, 0n),
    });
  }
  const at = (e: Entry) => (e.kind === "page" ? e.page.createdAt : e.latestAt);
  return entries.sort((a, b) => (at(a) === at(b) ? 0 : at(a) > at(b) ? -1 : 1));
}
