"use client";
import { Tooltip as Base } from "@base-ui/react/tooltip";
import type { ReactElement, ReactNode } from "react";

/** Supplementary label for icon-only controls. The trigger must still carry its own accessible name. */
export function Tooltip({
  label,
  children,
  side = "top",
}: {
  label: ReactNode;
  children: ReactElement;
  side?: "top" | "right" | "bottom" | "left";
}) {
  return (
    <Base.Root>
      <Base.Trigger render={children} />
      <Base.Portal>
        <Base.Positioner side={side} sideOffset={8}>
          <Base.Popup
            className={
              "origin-[var(--transform-origin)] rounded-[4px] bg-primary px-2 py-1 text-small text-on-primary shadow-[var(--shadow-float)] " +
              "transition-[opacity,transform] duration-[125ms] ease-out " +
              "data-[starting-style]:scale-[0.97] data-[starting-style]:opacity-0 data-[ending-style]:scale-[0.97] data-[ending-style]:opacity-0 " +
              "data-[instant]:transition-none"
            }
          >
            {label}
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  );
}

export const TooltipProvider = Base.Provider;
