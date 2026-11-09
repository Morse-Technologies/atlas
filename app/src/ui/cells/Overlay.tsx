"use client";
import { Dialog } from "@base-ui/react/dialog";
import { Drawer } from "@base-ui/react/drawer";
import { Menu as BaseMenu } from "@base-ui/react/menu";
import { X } from "lucide-react";
import { useEffect, useRef, type ReactElement, type ReactNode } from "react";
import { cn } from "@/lib/cn";

/**
 * Side sheet on the drawer curve. Swipe toward its edge to dismiss (the motion follows the finger and can be
 * reversed mid-way), and the back gesture or button closes it instead of leaving the page.
 */
export function Sheet({
  open, onOpenChange, title, description, children, side = "right", width = "w-[min(480px,100vw)]",
}: {
  open: boolean; onOpenChange: (o: boolean) => void; title: ReactNode; description?: ReactNode;
  children: ReactNode; side?: "right" | "left"; width?: string;
}) {
  usePredictiveBack(open, () => onOpenChange(false));
  const right = side === "right";
  return (
    <Drawer.Root open={open} onOpenChange={onOpenChange} swipeDirection={right ? "right" : "left"}>
      <Drawer.Portal>
        <Drawer.Backdrop className="fixed inset-0 z-40 bg-[var(--scrim)] opacity-[calc(1-var(--drawer-swipe-progress,0))] transition-opacity duration-[var(--dur-3)] ease-[var(--ease-drawer)] data-[ending-style]:opacity-0 data-[starting-style]:opacity-0 data-[swiping]:duration-0" />
        <Drawer.Viewport className="fixed inset-0 z-50 pointer-events-none">
          <Drawer.Popup
            className={cn(
              "pointer-events-auto fixed inset-y-0 flex flex-col bg-surface shadow-[var(--shadow-float)] outline-none",
              "pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)]",
              "[transform:translateX(var(--drawer-swipe-movement-x,0px))] transition-transform duration-[var(--dur-3)] ease-[var(--ease-drawer)]",
              "data-[swiping]:duration-0 data-[swiping]:select-none data-[ending-style]:duration-[200ms]",
              right
                ? "right-0 data-[starting-style]:[transform:translateX(100%)] data-[ending-style]:[transform:translateX(100%)]"
                : "left-0 data-[starting-style]:[transform:translateX(-100%)] data-[ending-style]:[transform:translateX(-100%)]",
              width,
            )}
          >
            <Drawer.Content className="flex min-h-0 flex-1 flex-col">
              <div className="flex items-start justify-between gap-4 border-b border-rule px-5 py-4">
                <div className="min-w-0">
                  <Drawer.Title className="text-section font-medium">{title}</Drawer.Title>
                  {description && <Drawer.Description className="mt-0.5 text-small text-muted">{description}</Drawer.Description>}
                </div>
                <Drawer.Close aria-label="Close" className="hit-slop -m-1 grid size-8 place-items-center rounded-control text-muted hover:bg-raised hover:text-text">
                  <X className="size-4" />
                </Drawer.Close>
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">{children}</div>
            </Drawer.Content>
          </Drawer.Popup>
        </Drawer.Viewport>
      </Drawer.Portal>
    </Drawer.Root>
  );
}

/** While an overlay is open, one history entry belongs to it: Back closes the overlay, not the page. */
function usePredictiveBack(open: boolean, close: () => void) {
  const closeRef = useRef(close);
  useEffect(() => { closeRef.current = close; });
  useEffect(() => {
    if (!open) return;
    // Keep the router's own state so a Back to this entry is a no-op for Next.js.
    history.pushState({ ...history.state, atlasOverlay: true }, "");
    const onPop = () => closeRef.current();
    window.addEventListener("popstate", onPop);
    return () => {
      window.removeEventListener("popstate", onPop);
      if (history.state?.atlasOverlay) history.back();
    };
  }, [open]);
}

/** A centred dialog that scales from 96 %. */
export function Modal({
  open, onOpenChange, title, children, className, labelHidden, instant,
}: {
  open: boolean; onOpenChange: (o: boolean) => void; title: string; children: ReactNode; className?: string; labelHidden?: boolean;
  /** Keyboard-opened, high-frequency surfaces (⌘K) skip the animation entirely. */
  instant?: boolean;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className={cn("fixed inset-0 z-40 bg-[var(--scrim)]", !instant && "transition-opacity duration-[var(--dur-2)] ease-out data-[starting-style]:opacity-0 data-[ending-style]:opacity-0")} />
        <Dialog.Popup
          className={cn(
            "fixed top-[12vh] left-1/2 z-50 w-[min(640px,calc(100vw-32px))] -translate-x-1/2 rounded-card border border-border bg-surface shadow-[var(--shadow-float)] outline-none",
            !instant && "transition-[opacity,transform] duration-[var(--dur-2)] ease-out data-[ending-style]:duration-[150ms]",
            !instant && "data-[starting-style]:scale-[0.96] data-[starting-style]:opacity-0 data-[ending-style]:scale-[0.96] data-[ending-style]:opacity-0",
            className,
          )}
        >
          <Dialog.Title className={labelHidden ? "sr-only" : "px-5 pt-4 text-section font-medium"}>{title}</Dialog.Title>
          {children}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** A dropdown menu; items close it on click. */
export function Menu({ trigger, items, align = "end" }: {
  trigger: ReactElement;
  items: ({ label: ReactNode; onClick: () => void; tone?: "danger"; icon?: ReactNode } | "separator")[];
  align?: "start" | "end";
}) {
  return (
    <BaseMenu.Root>
      <BaseMenu.Trigger render={trigger} />
      <BaseMenu.Portal>
        <BaseMenu.Positioner sideOffset={6} align={align}>
          <BaseMenu.Popup className="min-w-48 origin-[var(--transform-origin)] rounded-card border border-border bg-surface p-1 shadow-[var(--shadow-float)] outline-none transition-[opacity,transform] duration-[var(--dur-2)] ease-out data-[ending-style]:duration-[120ms] data-[starting-style]:scale-95 data-[starting-style]:opacity-0 data-[ending-style]:scale-95 data-[ending-style]:opacity-0">
            {items.map((it, i) =>
              it === "separator" ? (
                <div key={i} role="separator" className="my-1 h-px bg-rule" />
              ) : (
                <BaseMenu.Item
                  key={i}
                  onClick={it.onClick}
                  className={cn(
                    "flex h-8 cursor-default items-center gap-2 rounded-[5px] px-2.5 text-body outline-none select-none data-[highlighted]:bg-raised [&_svg]:size-4 [&_svg]:text-muted",
                    it.tone === "danger" && "text-negative",
                  )}
                >
                  {it.icon}
                  {it.label}
                </BaseMenu.Item>
              ),
            )}
          </BaseMenu.Popup>
        </BaseMenu.Positioner>
      </BaseMenu.Portal>
    </BaseMenu.Root>
  );
}
