import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { dashboardClient, sessionQueryKey, useSession } from "@/hooks/session";
import { errorMessage, isReauthRequired, login } from "@/lib/transport";
import { absolute, relative } from "@/lib/format";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from "@/components/ui/empty";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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

const tokensKey = ["tokens"] as const;
const HOUR = 3600;

const EXPIRY_CHOICES: { label: string; value: number }[] = [
  { label: "Never", value: 0 },
  { label: "30 days", value: 30 * 24 * HOUR },
  { label: "90 days", value: 90 * 24 * HOUR },
  { label: "1 year", value: 365 * 24 * HOUR },
];

export function Tokens() {
  const queryClient = useQueryClient();
  const { data: session } = useSession();
  const [creating, setCreating] = useState(false);
  const [revealed, setRevealed] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const { data, isPending, error } = useQuery({
    queryKey: tokensKey,
    queryFn: () => dashboardClient.listTokens({}),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: tokensKey });
    queryClient.invalidateQueries({ queryKey: sessionQueryKey });
  };

  /**
   * Minting needs a recently authenticated session. Rather than showing a
   * permission error, send the user through the identity provider and back to
   * this page, which is the action the error is actually asking for.
   */
  const onMintError = (err: unknown) => {
    if (isReauthRequired(err)) {
      toast.info("Confirming your identity before creating a token…");
      login("/tokens", session?.loginUrl);
      return;
    }
    toast.error(errorMessage(err));
  };

  const create = useMutation({
    mutationFn: (input: { name: string; expiresInSeconds: number }) =>
      dashboardClient.createToken({
        name: input.name,
        expiresInSeconds: BigInt(input.expiresInSeconds),
      }),
    onSuccess: (resp) => {
      setCreating(false);
      setRevealed(resp.token);
      invalidate();
    },
    onError: onMintError,
  });

  const rotate = useMutation({
    mutationFn: (id: string) => dashboardClient.rotateToken({ id }),
    onSuccess: (resp) => {
      setRevealed(resp.token);
      invalidate();
    },
    onError: onMintError,
  });

  const del = useMutation({
    mutationFn: (id: string) => dashboardClient.deleteToken({ id }),
    onSuccess: () => {
      toast.success("Token deleted");
      setPendingDelete(null);
      invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });

  const tokens = data?.tokens ?? [];

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-heading text-xl font-semibold tracking-tight">
            CLI tokens
          </h1>
          <p className="mt-1 text-xs/relaxed text-muted-foreground">
            Used by <code className="text-primary">page-report login</code>. A
            token is shown once, when it is created.
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <Plus />
          New token
        </Button>
      </div>

      <Card className="py-0">
        {isPending ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-7 w-full" />
            <Skeleton className="h-7 w-full" />
          </div>
        ) : error ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>Could not load tokens</EmptyTitle>
              <EmptyDescription>{errorMessage(error)}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : tokens.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No tokens yet</EmptyTitle>
              <EmptyDescription>
                Create one, then run{" "}
                <code className="text-primary">page-report login</code> and
                paste it in.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Token</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">
                    {t.name}
                    {t.revoked && (
                      <span className="ml-2">
                        <Badge variant="destructive">revoked</Badge>
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <code className="text-[0.625rem] text-muted-foreground">
                      {t.displayPrefix}…
                    </code>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    <span title={absolute(t.createdAt)}>
                      {relative(t.createdAt)}
                    </span>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    {relative(t.lastUsedAt, "never")}
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    {absolute(t.expiresAt, "never")}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      <IconAction
                        label="Rotate: issue a new secret, invalidating the old one"
                        disabled={rotate.isPending}
                        onClick={() => rotate.mutate(t.id)}
                      >
                        <RefreshCw />
                      </IconAction>
                      <IconAction
                        label="Delete token"
                        className="hover:text-destructive"
                        onClick={() =>
                          setPendingDelete({ id: t.id, name: t.name })
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

      <CreateTokenDialog
        open={creating}
        pending={create.isPending}
        defaultExpiry={Number(session?.tokenDefaultTtlSeconds ?? 0)}
        onClose={() => setCreating(false)}
        onSubmit={(name, expiresInSeconds) =>
          create.mutate({ name, expiresInSeconds })
        }
      />

      <RevealTokenDialog token={revealed} onClose={() => setRevealed(null)} />

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete token</AlertDialogTitle>
            <AlertDialogDescription>
              Delete{" "}
              <span className="text-foreground">{pendingDelete?.name}</span>?
              Any machine using it stops being able to reach the API
              immediately.
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
  disabled,
  className,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
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
            disabled={disabled}
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

function CreateTokenDialog({
  open,
  pending,
  defaultExpiry,
  onClose,
  onSubmit,
}: {
  open: boolean;
  pending: boolean;
  defaultExpiry: number;
  onClose: () => void;
  onSubmit: (name: string, expiresInSeconds: number) => void;
}) {
  const [name, setName] = useState("");
  const [expiry, setExpiry] = useState(defaultExpiry);

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New CLI token</DialogTitle>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit(name.trim(), expiry);
          }}
          className="space-y-4"
        >
          <Field>
            <FieldLabel htmlFor="token-name">Name</FieldLabel>
            <Input
              id="token-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="laptop"
              maxLength={64}
              autoFocus
              required
            />
            <FieldDescription>
              A label so you can tell your tokens apart later.
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="token-expiry">Expires</FieldLabel>
            <Select
              items={EXPIRY_CHOICES}
              value={expiry}
              onValueChange={(next) => setExpiry(Number(next))}
            >
              <SelectTrigger id="token-expiry" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {EXPIRY_CHOICES.map((c) => (
                  <SelectItem key={c.value} value={c.value}>
                    {c.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || name.trim() === ""}>
              Create token
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RevealTokenDialog({
  token,
  onClose,
}: {
  token: string | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={token !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Copy your token now</DialogTitle>
          <DialogDescription>
            Only a hash of this token is stored, so this is the one and only
            time it can be shown. If you lose it, rotate the token to get a new
            one.
          </DialogDescription>
        </DialogHeader>

        {/* min-w-0 on both: without it the flex item refuses to shrink below
            the token's intrinsic width and the dialog overflows. */}
        <div className="flex min-w-0 items-center gap-2">
          <code className="min-w-0 flex-1 overflow-x-auto rounded-md border border-input bg-input/20 px-2 py-1.5 text-[0.625rem] whitespace-nowrap">
            {token}
          </code>
          <Button
            variant="outline"
            onClick={() => {
              if (token) navigator.clipboard.writeText(token);
              toast.success("Token copied");
            }}
          >
            <Copy />
            Copy
          </Button>
        </div>

        <p className="text-xs/relaxed text-muted-foreground">
          On the machine that needs it, run{" "}
          <code className="text-primary">page-report login</code> and paste it
          at the prompt.
        </p>

        <DialogFooter>
          <DialogClose render={<Button />}>Done</DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
