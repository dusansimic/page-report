import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui";

/**
 * Built on the native <dialog> element so focus trapping, Escape handling and
 * the top layer come from the platform rather than from a dependency.
 */
export function Modal({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      onClose={onClose}
      // Clicking the backdrop (the dialog element itself, outside its content)
      // dismisses, matching what people expect of a modal.
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-lg border
        border-line bg-panel p-0 text-fg backdrop:bg-black/70"
    >
      <div className="flex items-center justify-between border-b border-line px-5 py-3">
        <h2 className="font-medium">{title}</h2>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClose}
          aria-label="Close"
          className="px-2"
        >
          <X className="size-4" />
        </Button>
      </div>
      <div className="px-5 py-4">{children}</div>
    </dialog>
  );
}
