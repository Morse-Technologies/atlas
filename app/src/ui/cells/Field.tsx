"use client";
import { useId, type ReactNode } from "react";

/** Label above the control, help and error tied to it with aria-describedby. */
export function Field({
  label,
  help,
  error,
  children,
}: {
  label: ReactNode;
  help?: ReactNode;
  error?: ReactNode;
  children: (ids: { id: string; describedBy?: string; invalid: boolean }) => ReactNode;
}) {
  const id = useId();
  const helpId = help ? `${id}-help` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [helpId, errorId].filter(Boolean).join(" ") || undefined;
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-body font-medium">{label}</label>
      {children({ id, describedBy, invalid: !!error })}
      {help && !error && <p id={helpId} className="text-small text-muted">{help}</p>}
      {error && <p id={errorId} className="text-small text-negative">{error}</p>}
    </div>
  );
}
