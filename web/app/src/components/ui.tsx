import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * A small set of shared primitives. The app has three screens and two tables,
 * so a full component library would be more surface than the UI needs; these
 * cover it while keeping one place to change the styling.
 */

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium " +
    "transition-colors disabled:pointer-events-none disabled:opacity-50 " +
    "whitespace-nowrap cursor-pointer",
  {
    variants: {
      variant: {
        primary: "bg-accent text-bg hover:bg-accent/90",
        secondary: "border border-line bg-panel text-fg hover:border-muted",
        ghost: "text-muted hover:text-fg hover:bg-panel",
        danger: "border border-bad/40 text-bad hover:bg-bad/10",
      },
      size: {
        sm: "h-8 px-3",
        md: "h-9 px-4",
        lg: "h-11 px-6 text-base",
      },
    },
    defaultVariants: { variant: "primary", size: "md" },
  },
);

export function Button({
  className,
  variant,
  size,
  ...props
}: ComponentProps<"button"> & VariantProps<typeof buttonVariants>) {
  return (
    <button
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
}

export function LinkButton({
  className,
  variant,
  size,
  ...props
}: ComponentProps<"a"> & VariantProps<typeof buttonVariants>) {
  return (
    <a
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
}

export function Card({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("rounded-lg border border-line bg-panel", className)}
      {...props}
    />
  );
}

export function Input({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "h-9 w-full rounded-md border border-line bg-bg px-3 text-sm",
        "placeholder:text-muted focus:border-accent focus:outline-none",
        className,
      )}
      {...props}
    />
  );
}

export function Select({ className, ...props }: ComponentProps<"select">) {
  return (
    <select
      className={cn(
        "h-9 w-full rounded-md border border-line bg-bg px-3 text-sm",
        "focus:border-accent focus:outline-none",
        className,
      )}
      {...props}
    />
  );
}

export function Badge({
  tone = "muted",
  children,
}: {
  tone?: "muted" | "good" | "warn" | "bad";
  children: ReactNode;
}) {
  const tones = {
    muted: "border-line text-muted",
    good: "border-good/40 text-good",
    warn: "border-warn/40 text-warn",
    bad: "border-bad/40 text-bad",
  } as const;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full border px-2 py-0.5 text-xs",
        tones[tone],
      )}
    >
      {children}
    </span>
  );
}

export function Table({ className, ...props }: ComponentProps<"table">) {
  return (
    <div className="overflow-x-auto">
      <table className={cn("w-full text-sm", className)} {...props} />
    </div>
  );
}

export function Th({ className, ...props }: ComponentProps<"th">) {
  return (
    <th
      className={cn(
        "border-b border-line px-4 py-2 text-left text-xs font-semibold",
        "uppercase tracking-wide text-muted",
        className,
      )}
      {...props}
    />
  );
}

export function Td({ className, ...props }: ComponentProps<"td">) {
  return (
    <td
      className={cn("border-b border-line px-4 py-3 align-middle", className)}
      {...props}
    />
  );
}

export function EmptyState({
  title,
  children,
}: {
  title: string;
  children?: ReactNode;
}) {
  return (
    <div className="px-4 py-12 text-center">
      <p className="font-medium">{title}</p>
      {children && <div className="mt-2 text-sm text-muted">{children}</div>}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <div className={cn("animate-pulse rounded bg-line/60", className)} />
  );
}
