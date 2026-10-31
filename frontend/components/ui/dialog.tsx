"use client";

import { useEffect, useId, useRef, type ReactNode } from "react";
import { Button } from ".";

export function Dialog({
  open,
  title,
  onClose,
  children,
  className = "",
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const labelId = useId();
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog || !open) return;
    const trigger = document.activeElement as HTMLElement | null;
    dialog.showModal();
    return () => {
      dialog.close();
      trigger?.focus();
    };
  }, [open]);

  return (
    <dialog
      ref={ref}
      aria-labelledby={labelId}
      className={className}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onKeyDown={(event) => {
        if (event.key !== "Tab") return;
        const elements = Array.from(
          ref.current?.querySelectorAll<HTMLElement>(
            'a[href], button:not([disabled]), select, input, [tabindex="0"]',
          ) ?? [],
        );
        const first = elements[0];
        const last = elements[elements.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        }
        if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }}
    >
      <div className="dialog-heading">
        <h2 id={labelId}>{title}</h2>
        <Button
          className="button-quiet"
          onClick={onClose}
          aria-label={`Close ${title.toLowerCase()}`}
        >
          Close <span aria-hidden="true">×</span>
        </Button>
      </div>
      {children}
    </dialog>
  );
}
