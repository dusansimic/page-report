import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Link2, Trash2 } from "lucide-react";
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

  const pages = data?.pages ?? [];

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
        ) : pages.length === 0 ? (
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
              {pages.map((p) => (
                <TableRow key={p.id}>
                  <TableCell>
                    <div className="font-medium">{p.title || "Untitled"}</div>
                    <div className="text-[0.625rem] text-muted-foreground">
                      {p.id}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    <span title={absolute(p.createdAt)}>
                      {relative(p.createdAt)}
                    </span>
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
                        onClick={() =>
                          setPendingDelete({
                            id: p.id,
                            title: p.title || p.id,
                          })
                        }
                      >
                        <Trash2 />
                      </IconAction>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
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
