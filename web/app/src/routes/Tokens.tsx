import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { dashboardClient, sessionQueryKey, useSession } from "@/hooks/session";
import { errorMessage, isReauthRequired, login } from "@/lib/transport";
import { absolute, relative } from "@/lib/format";
import { Modal } from "@/components/Modal";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  Input,
  Select,
  Skeleton,
  Table,
  Td,
  Th,
} from "@/components/ui";

const tokensKey = ["tokens"] as const;
const HOUR = 3600;

const EXPIRY_CHOICES: { label: string; seconds: number }[] = [
  { label: "Never", seconds: 0 },
  { label: "30 days", seconds: 30 * 24 * HOUR },
  { label: "90 days", seconds: 90 * 24 * HOUR },
  { label: "1 year", seconds: 365 * 24 * HOUR },
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
          <h1 className="text-2xl font-semibold tracking-tight">CLI tokens</h1>
          <p className="mt-1 text-sm text-muted">
            Used by <code className="text-accent">page-report login</code>. A
            token is shown once, when it is created.
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <Plus className="size-4" />
          New token
        </Button>
      </div>

      <Card>
        {isPending ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : error ? (
          <EmptyState title="Could not load tokens">
            {errorMessage(error)}
          </EmptyState>
        ) : tokens.length === 0 ? (
          <EmptyState title="No tokens yet">
            Create one, then run{" "}
            <code className="text-accent">page-report login</code> and paste it
            in.
          </EmptyState>
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>Name</Th>
                <Th>Token</Th>
                <Th>Created</Th>
                <Th>Last used</Th>
                <Th>Expires</Th>
                <Th className="text-right">Actions</Th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((t) => (
                <tr key={t.id} className="last:[&>td]:border-0">
                  <Td className="font-medium">
                    {t.name}
                    {t.revoked && (
                      <span className="ml-2">
                        <Badge tone="bad">revoked</Badge>
                      </span>
                    )}
                  </Td>
                  <Td>
                    <code className="text-xs text-muted">
                      {t.displayPrefix}…
                    </code>
                  </Td>
                  <Td className="whitespace-nowrap text-muted">
                    <span title={absolute(t.createdAt)}>
                      {relative(t.createdAt)}
                    </span>
                  </Td>
                  <Td className="whitespace-nowrap text-muted">
                    {relative(t.lastUsedAt, "never")}
                  </Td>
                  <Td className="whitespace-nowrap text-muted">
                    {absolute(t.expiresAt, "never")}
                  </Td>
                  <Td>
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={rotate.isPending}
                        onClick={() => rotate.mutate(t.id)}
                        title="Rotate: issue a new secret, invalidating the old one"
                      >
                        <RefreshCw className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="hover:text-bad"
                        onClick={() =>
                          setPendingDelete({ id: t.id, name: t.name })
                        }
                        title="Delete token"
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

      <Modal
        open={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        title="Delete token"
      >
        <p className="text-sm text-muted">
          Delete <span className="text-fg">{pendingDelete?.name}</span>? Any
          machine using it stops being able to reach the API immediately.
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
  const [expiry, setExpiry] = useState(String(defaultExpiry));

  return (
    <Modal open={open} onClose={onClose} title="New CLI token">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit(name.trim(), Number(expiry));
        }}
        className="space-y-4"
      >
        <div className="space-y-1.5">
          <label htmlFor="token-name" className="text-sm">
            Name
          </label>
          <Input
            id="token-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="laptop"
            maxLength={64}
            autoFocus
            required
          />
          <p className="text-xs text-muted">
            A label so you can tell your tokens apart later.
          </p>
        </div>

        <div className="space-y-1.5">
          <label htmlFor="token-expiry" className="text-sm">
            Expires
          </label>
          <Select
            id="token-expiry"
            value={expiry}
            onChange={(e) => setExpiry(e.target.value)}
          >
            {EXPIRY_CHOICES.map((c) => (
              <option key={c.seconds} value={c.seconds}>
                {c.label}
              </option>
            ))}
          </Select>
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={pending || name.trim() === ""}>
            Create token
          </Button>
        </div>
      </form>
    </Modal>
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
    <Modal open={token !== null} onClose={onClose} title="Copy your token now">
      <p className="text-sm text-muted">
        Only a hash of this token is stored, so this is the one and only time it
        can be shown. If you lose it, rotate the token to get a new one.
      </p>
      <div className="mt-4 flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded-md border border-line bg-bg px-3 py-2 text-xs">
          {token}
        </code>
        <Button
          variant="secondary"
          onClick={() => {
            if (token) navigator.clipboard.writeText(token);
            toast.success("Token copied");
          }}
        >
          <Copy className="size-4" />
          Copy
        </Button>
      </div>
      <p className="mt-4 text-sm text-muted">
        On the machine that needs it, run{" "}
        <code className="text-accent">page-report login</code> and paste it at
        the prompt.
      </p>
      <div className="mt-5 flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </div>
    </Modal>
  );
}
