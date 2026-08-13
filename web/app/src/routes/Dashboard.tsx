import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Link2, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { dashboardClient } from "@/hooks/session";
import { errorMessage } from "@/lib/transport";
import { absolute, bytes, relative } from "@/lib/format";
import { Modal } from "@/components/Modal";
import {
  Button,
  Card,
  EmptyState,
  Skeleton,
  Table,
  Td,
  Th,
} from "@/components/ui";

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
        <h1 className="text-2xl font-semibold tracking-tight">Reports</h1>
        <p className="mt-1 text-sm text-muted">
          Everything you have published to this server.
        </p>
      </div>

      <Card>
        {isPending ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : error ? (
          <EmptyState title="Could not load reports">
            {errorMessage(error)}
          </EmptyState>
        ) : pages.length === 0 ? (
          <EmptyState title="No reports yet">
            Publish one with{" "}
            <code className="text-accent">page-report upload report.html</code>.
          </EmptyState>
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>Title</Th>
                <Th>Created</Th>
                <Th>Size</Th>
                <Th className="text-right">Actions</Th>
              </tr>
            </thead>
            <tbody>
              {pages.map((p) => (
                <tr key={p.id} className="last:[&>td]:border-0">
                  <Td>
                    <div className="font-medium">{p.title || "Untitled"}</div>
                    <div className="text-xs text-muted">{p.id}</div>
                  </Td>
                  <Td className="whitespace-nowrap text-muted">
                    <span title={absolute(p.createdAt)}>
                      {relative(p.createdAt)}
                    </span>
                  </Td>
                  <Td className="whitespace-nowrap text-muted">
                    {bytes(p.sizeBytes)}
                  </Td>
                  <Td>
                    <div className="flex items-center justify-end gap-1">
                      {/*
                        Reports open as top-level documents, never inside this
                        app: the sandbox that keeps them inert depends on it.
                      */}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => window.open(p.url, "_blank", "noopener")}
                        title="Open report"
                      >
                        <ExternalLink className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          navigator.clipboard.writeText(p.url);
                          toast.success("Link copied");
                        }}
                        title="Copy link"
                      >
                        <Link2 className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="hover:text-bad"
                        onClick={() =>
                          setPendingDelete({
                            id: p.id,
                            title: p.title || p.id,
                          })
                        }
                        title="Delete report"
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      <Modal
        open={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        title="Delete report"
      >
        <p className="text-sm text-muted">
          Delete <span className="text-fg">{pendingDelete?.title}</span>? The
          link stops working immediately and the HTML is removed from the
          server.
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setPendingDelete(null)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            disabled={del.isPending}
            onClick={() => pendingDelete && del.mutate(pendingDelete.id)}
          >
            Delete
          </Button>
        </div>
      </Modal>
    </div>
  );
}
