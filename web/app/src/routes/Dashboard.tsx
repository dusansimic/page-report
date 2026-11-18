import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
  Link2,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";

import { dashboardClient } from "@/hooks/session";
import { errorMessage } from "@/lib/transport";
import { absolute, bytes, relative } from "@/lib/format";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import type { PageMeta } from "@/gen/pagereport/v1/pagereport_pb";
import { groupPages, sourceLabel, type GroupEntry } from "@/lib/group";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const pagesKey = ["pages"] as const;
const expandedKey = "pr.expandedSessions";

/** null means "nothing stored": the caller picks the default. */
function loadExpanded(): Set<string> | null {
  try {
    const raw = localStorage.getItem(expandedKey);
    if (raw === null) return null;
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v)
      ? new Set(v.filter((x): x is string => typeof x === "string"))
      : null;
  } catch {
    return null;
  }
}

export function Dashboard() {
  const queryClient = useQueryClient();
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    title: string;
  } | null>(null);

  const { data, isPending, error } = useQuery({
    queryKey: pagesKey,
    queryFn: () => dashboardClient.listPages({}),
  });

  const del = useMutation({
    mutationFn: (id: string) => dashboardClient.deletePage({ id }),
    onSuccess: () => {
      toast.success("Report deleted");
      setPendingDelete(null);
      queryClient.invalidateQueries({ queryKey: pagesKey });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });

  const entries = useMemo(() => groupPages(data?.pages ?? []), [data]);
  const [stored, setStored] = useState(loadExpanded);
  // Until the user toggles something, the newest group is open.
  const expanded = useMemo(() => {
    if (stored) return stored;
    const first = entries.find((e) => e.kind === "group");
    return new Set(first ? [first.key] : []);
  }, [stored, entries]);

  const toggle = (key: string) => {
    const next = new Set(expanded);
    if (!next.delete(key)) next.add(key);
    setStored(next);
    try {
      localStorage.setItem(expandedKey, JSON.stringify([...next]));
    } catch {
      // storage unavailable: state still works for this visit
    }
  };
  const askDelete = (p: PageMeta) =>
    setPendingDelete({ id: p.id, title: p.title || p.id });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-heading text-xl font-semibold tracking-tight">
          Reports
        </h1>
        <p className="mt-1 text-xs/relaxed text-muted-foreground">
          Everything you have published to this server.
        </p>
      </div>

      <Card className="py-0">
        {isPending ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-7 w-full" />
            <Skeleton className="h-7 w-full" />
            <Skeleton className="h-7 w-full" />
          </div>
        ) : error ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>Could not load reports</EmptyTitle>
              <EmptyDescription>{errorMessage(error)}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : entries.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No reports yet</EmptyTitle>
              <EmptyDescription>
                Publish one with{" "}
                <code className="text-primary">
                  page-report upload report.html
                </code>
                .
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Title</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Size</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {entries.map((e) =>
                e.kind === "page" ? (
                  <PageRow
                    key={e.page.id}
                    page={e.page}
                    showBadge
                    onDelete={askDelete}
                  />
                ) : (
                  <GroupRows
                    key={e.key}
                    group={e}
                    expanded={expanded.has(e.key)}
                    onToggle={() => toggle(e.key)}
                    onDelete={askDelete}
                  />
                ),
              )}
            </TableBody>
          </Table>
        )}
      </Card>

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete report</AlertDialogTitle>
            <AlertDialogDescription>
              Delete{" "}
              <span className="text-foreground">{pendingDelete?.title}</span>?
              The link stops working immediately and the HTML is removed from
              the server.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={del.isPending}
              onClick={() => pendingDelete && del.mutate(pendingDelete.id)}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** An icon-only button plus the tooltip that says what it does. */
function IconAction({
  label,
  onClick,
  className,
  children,
}: {
  label: string;
  onClick: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className={className}
            onClick={onClick}
            aria-label={label}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function SessionBadge({ source }: { source: string }) {
  return (
    <Badge variant="secondary" className="font-normal">
      {sourceLabel(source)}
    </Badge>
  );
}

/** One report row. `indent` marks a child row inside an expanded group. */
function PageRow({
  page: p,
  indent,
  showBadge,
  onDelete,
}: {
  page: PageMeta;
  indent?: boolean;
  showBadge?: boolean;
  onDelete: (p: PageMeta) => void;
}) {
  return (
    <TableRow>
      <TableCell className={indent ? "pl-9" : undefined}>
        <div className="flex items-center gap-2">
          <span className="font-medium">{p.title || "Untitled"}</span>
          {showBadge && p.session?.id ? (
            <SessionBadge source={p.session.source} />
          ) : null}
        </div>
        <div className="text-[0.625rem] text-muted-foreground">{p.id}</div>
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        <span title={absolute(p.createdAt)}>{relative(p.createdAt)}</span>
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {bytes(p.sizeBytes)}
      </TableCell>
      <TableCell>
        <div className="flex items-center justify-end gap-1">
          {/*
            Reports open as top-level documents, never inside this
            app: the sandbox that keeps them inert depends on it.
          */}
          <IconAction
            label="Open report"
            onClick={() => window.open(p.url, "_blank", "noopener")}
          >
            <ExternalLink />
          </IconAction>
          <IconAction
            label="Copy link"
            onClick={() => {
              navigator.clipboard.writeText(p.url);
              toast.success("Link copied");
            }}
          >
            <Link2 />
          </IconAction>
          <IconAction
            label="Delete report"
            className="hover:text-destructive"
            onClick={() => onDelete(p)}
          >
            <Trash2 />
          </IconAction>
        </div>
      </TableCell>
    </TableRow>
  );
}

/** Header row for a session group, followed by its reports when expanded. */
function GroupRows({
  group,
  expanded,
  onToggle,
  onDelete,
}: {
  group: GroupEntry;
  expanded: boolean;
  onToggle: () => void;
  onDelete: (p: PageMeta) => void;
}) {
  const title = group.title || `${sourceLabel(group.source)} session`;
  return (
    <>
      <TableRow className="bg-muted/30">
        <TableCell>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-expanded={expanded}
              aria-label={`${expanded ? "Collapse" : "Expand"} ${title}`}
              onClick={onToggle}
            >
              {expanded ? <ChevronDown /> : <ChevronRight />}
            </Button>
            <span className="font-medium">{title}</span>
            <SessionBadge source={group.source} />
            <span className="text-muted-foreground">
              {group.pages.length} reports
            </span>
          </div>
        </TableCell>
        <TableCell className="whitespace-nowrap text-muted-foreground">
          <span title={absolute(group.latestAt)}>
            {relative(group.latestAt)}
          </span>
        </TableCell>
        <TableCell className="whitespace-nowrap text-muted-foreground">
          {bytes(group.totalBytes)}
        </TableCell>
        <TableCell>
          <div className="flex items-center justify-end gap-1">
            <IconAction
              label="Copy all links"
              onClick={() => {
                navigator.clipboard.writeText(
                  group.pages.map((p) => p.url).join("\n"),
                );
                toast.success(`${group.pages.length} links copied`);
              }}
            >
              <Link2 />
            </IconAction>
          </div>
        </TableCell>
      </TableRow>
      {expanded
        ? group.pages.map((p) => (
            <PageRow key={p.id} page={p} indent onDelete={onDelete} />
          ))
        : null}
    </>
  );
}
