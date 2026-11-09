"use client";
import Link from "next/link";
import type { LucideIcon } from "lucide-react";
import { Ellipsis, Lock, Moon, PanelLeftClose, PanelLeftOpen, Sun } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type ComponentProps, type FocusEvent, type MouseEvent, type PointerEvent, type ReactNode } from "react";
import { inRect, onTheWay, type Pt } from "@/lib/safeTriangle";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import { useGlide, usePopOnIncrease } from "@/lib/gsapIn";
import { Avatar, Mark } from "@/ui/atoms";
import { Menu } from "@/ui/cells";
import { HOME_PATH } from "@/lib/edition";

export type NavItem = { href: string; label: string; icon: LucideIcon; count?: number; locked?: boolean };

const isActive = (pathname: string, href: string) => pathname === href || pathname.startsWith(`${href}/`);

/** True only after hydration, so client-only portals never take part in it. */
const noop = () => () => {};
const useHydrated = () => useSyncExternalStore(noop, () => true, () => false);

/* One shared tooltip that follows the cursor and glides between items (collapsed rail only). ---------------- */
function useCursorTip(enabled: boolean) {
  const ref = useRef<HTMLDivElement>(null);
  const [tip, setTip] = useState<{ label: string; visible: boolean }>({ label: "", visible: false });
  const shown = useRef(false);
  const hydrated = useHydrated();

  const place = useCallback((x: number, y: number) => {
    const el = ref.current;
    if (!el) return;
    // First appearance jumps into place; after that it glides.
    el.style.transitionProperty = shown.current ? "transform, opacity, scale" : "opacity, scale";
    el.style.transform = `translate3d(${Math.round(x)}px, ${Math.round(y)}px, 0)`;
    shown.current = true;
  }, []);

  const hide = useCallback(() => { shown.current = false; setTip((t) => ({ ...t, visible: false })); }, []);

  const onPointerMove = useCallback((e: PointerEvent) => {
    if (!enabled || e.pointerType === "touch") return;
    const item = (e.target as Element).closest<HTMLElement>("[data-tip]");
    if (!item) return hide();
    const label = item.dataset.tip!;
    setTip((t) => (t.label === label && t.visible ? t : { label, visible: true }));
    place(e.clientX + 14, e.clientY - 38);
  }, [enabled, hide, place]);

  const onFocus = useCallback((e: FocusEvent) => {
    if (!enabled) return;
    const item = (e.target as Element).closest<HTMLElement>("[data-tip]");
    if (!item || !item.matches(":focus-visible")) return;
    const r = item.getBoundingClientRect();
    setTip({ label: item.dataset.tip!, visible: true });
    place(r.right + 10, r.top + r.height / 2 - 14);
  }, [enabled, place]);

  const node = !hydrated ? null : createPortal(
    <div
      ref={ref}
      aria-hidden
      data-visible={enabled && tip.visible}
      className={cn(
        "pointer-events-none fixed top-0 left-0 z-50 rounded-[6px] bg-primary px-2.5 py-1 text-small font-medium whitespace-nowrap text-on-primary shadow-[var(--shadow-float)]",
        "origin-bottom-left scale-[0.96] opacity-0 duration-[140ms] ease-out data-[visible=true]:scale-100 data-[visible=true]:opacity-100",
      )}
    >
      <span key={tip.label} className="block animate-token-in">{tip.label}</span>
    </div>,
    document.body,
  );

  return { node, handlers: { onPointerMove, onPointerLeave: hide, onFocus, onBlur: hide } };
}

/* Sidebar ----------------------------------------------------------------------------------------- */
const item =
  "relative mx-2 flex h-10 shrink-0 items-center gap-3 rounded-card px-[11px] text-body text-muted " +
  "transition-[background-color,color,transform] duration-[var(--dur-1)] ease-out hover:bg-active/60 hover:text-text active:scale-[0.98]";

function Label({ expanded, children }: { expanded: boolean; children: ReactNode }) {
  return (
    <span className={cn(
      "min-w-0 truncate whitespace-nowrap transition-[opacity,transform] ease-out",
      expanded ? "translate-x-0 opacity-100 delay-[80ms] duration-[var(--dur-2)]" : "-translate-x-1 opacity-0 duration-[100ms]",
    )}>
      {children}
    </span>
  );
}

function NavLink({ it, pathname, expanded, onIntent, peekRef }: {
  it: NavItem; pathname: string; expanded: boolean; onIntent?: (href: string) => void;
  peekRef?: (el: HTMLAnchorElement | null) => void;
}) {
  const active = isActive(pathname, it.href);
  const badge = useRef<HTMLSpanElement>(null);
  usePopOnIncrease(badge, it.count);
  const name = it.locked ? `${it.label}, Atlas Pro` : it.count ? `${it.label}, ${it.count} open` : it.label;
  return (
    <Link href={it.href} ref={peekRef} data-tip={name} aria-label={expanded ? undefined : name} aria-current={active ? "page" : undefined}
      onPointerEnter={() => onIntent?.(it.href)} onFocus={() => onIntent?.(it.href)}
      className={cn(item, active && "text-text hover:bg-transparent")}>
      <it.icon className="size-[18px] flex-none" strokeWidth={1.6} />
      <Label expanded={expanded}>{it.label}</Label>
      {it.count ? (
        <span ref={badge} className={cn(
          "absolute grid h-4 min-w-4 place-items-center rounded-full bg-text px-1 pt-px text-[10px] leading-none font-bold text-surface num transition-[left,top,right] duration-[var(--dur-3)] ease-in-out",
          expanded ? "top-3 right-2.5" : "top-0.5 left-6",
        )}>{it.count}</span>
      ) : null}
      {it.locked && <Lock aria-hidden className={cn("absolute size-2.5", expanded ? "top-[15px] right-3" : "bottom-1.5 left-[26px]")} strokeWidth={2.5} />}
    </Link>
  );
}

/** A hover preview beside a rail item, held open along the safe triangle while the pointer travels to it. */
const PEEK_OPEN_DELAY_MS = 220; // long enough to ignore a pointer passing by

function usePeek(enabled: boolean) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const trigger = useRef<HTMLAnchorElement | null>(null);
  const panel = useRef<HTMLDivElement | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const leave = useRef<Pt | null>(null);

  const close = useCallback(() => { clearTimeout(timer.current); setOpen(false); leave.current = null; }, []);

  const bind = useCallback((el: HTMLAnchorElement | null) => {
    trigger.current = el;
    if (!el) return;
    el.onpointerenter = (e) => {
      if (!enabled || e.pointerType === "touch") return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => { setAnchor(el.getBoundingClientRect()); setOpen(true); }, PEEK_OPEN_DELAY_MS);
    };
    el.onpointerleave = (e) => { clearTimeout(timer.current); leave.current = { x: e.clientX, y: e.clientY }; };
  }, [enabled]);

  useEffect(() => {
    if (!open) return;
    let stall: ReturnType<typeof setTimeout> | undefined;
    const onMove = (e: globalThis.PointerEvent) => {
      const p = { x: e.clientX, y: e.clientY };
      const t = trigger.current?.getBoundingClientRect();
      const f = panel.current?.getBoundingClientRect();
      if (!t || !f) return;
      clearTimeout(stall);
      if (inRect(p, f, 2) || inRect(p, t, 2)) { leave.current = null; return; }
      if (leave.current && onTheWay(p, leave.current, f)) { stall = setTimeout(close, 350); return; } // on the way: wait
      close();
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") close(); };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("keydown", onKey);
    return () => { document.removeEventListener("pointermove", onMove); document.removeEventListener("keydown", onKey); clearTimeout(stall); };
  }, [open, close]);

  return [open && enabled, anchor, bind, panel, close] as const;
}

/** Collapsed (56px icon rail) or expanded (232px with labels); width moves on the in-out curve. */
export function AppSidebar({
  groups, footer, pathname, user, expanded, onToggleExpanded, theme, onToggleTheme, accountItems, onIntent, peek,
}: {
  groups: NavItem[][]; footer: NavItem[]; pathname: string; user?: { name: string; email: string; hue: number };
  expanded: boolean; onToggleExpanded: () => void;
  theme: "light" | "dark"; onToggleTheme: (e: MouseEvent) => void; accountItems: ComponentProps<typeof Menu>["items"];
  onIntent?: (href: string) => void;
  /** Optional hover preview for one item (collapsed rail only). */
  peek?: { href: string; content: ReactNode };
}) {
  const [peekOpen, peekAnchor, bindPeek, peekPanel, closePeek] = usePeek(!expanded && !!peek);
  const { node, handlers } = useCursorTip(!expanded && !peekOpen);
  const hydrated = useHydrated();
  const navRef = useRef<HTMLElement>(null);
  const pill = useRef<HTMLSpanElement>(null);
  // One highlight glides to the current page instead of each item lighting up on its own.
  useGlide(pill, () => {
    const a = navRef.current?.querySelector<HTMLElement>('a[aria-current="page"]');
    return a ? { y: a.offsetTop } : null;
  }, [pathname, expanded]);
  return (
    <nav
      ref={navRef}
      aria-label="Main"
      data-expanded={expanded}
      {...handlers}
      className="sticky top-0 hidden h-dvh flex-none flex-col overflow-x-hidden overflow-y-auto py-3 transition-[width] duration-[var(--dur-3)] ease-in-out md:flex"
      style={{ width: expanded ? 232 : 56 }}
    >
      <span ref={pill} aria-hidden className="pointer-events-none absolute top-0 right-2 left-2 h-10 rounded-card bg-active opacity-0" />
      <Link href={HOME_PATH} data-tip="Atlas" aria-label="Atlas home" className="mx-2 mb-1 flex h-10 shrink-0 items-center gap-2.5 rounded-card px-1.5 text-text">
        <Mark className="size-7 flex-none" />
        <Label expanded={expanded}><span className="text-section font-medium">Atlas</span></Label>
      </Link>
      {groups.map((g, i) => (
        <div key={i} className="flex flex-col gap-0.5">
          <span aria-hidden className={cn("my-2 h-px shrink-0 bg-control transition-[width,margin] duration-[var(--dur-3)] ease-in-out", expanded ? "mx-3.5 w-[204px]" : "mx-[18px] w-5")} />
          {g.map((it) => (
            <NavLink key={it.href} it={it} pathname={pathname} expanded={expanded} onIntent={onIntent}
              peekRef={peek?.href === it.href ? bindPeek : undefined} />
          ))}
        </div>
      ))}
      <div className="mt-auto flex flex-col gap-0.5 pt-4">
        {footer.map((it) => <NavLink key={it.href} it={it} pathname={pathname} expanded={expanded} />)}
        <button type="button" onClick={onToggleTheme} data-tip={theme === "dark" ? "Light theme" : "Dark theme"}
          aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"} className={item}>
          {theme === "dark" ? <Sun className="size-[18px] flex-none" strokeWidth={1.6} /> : <Moon className="size-[18px] flex-none" strokeWidth={1.6} />}
          <Label expanded={expanded}>{theme === "dark" ? "Light theme" : "Dark theme"}</Label>
        </button>
        <button type="button" onClick={onToggleExpanded} data-tip="Expand sidebar" aria-expanded={expanded}
          aria-label={expanded ? "Collapse sidebar" : "Expand sidebar"} className={item}>
          {expanded ? <PanelLeftClose className="size-[18px] flex-none" strokeWidth={1.6} /> : <PanelLeftOpen className="size-[18px] flex-none" strokeWidth={1.6} />}
          <Label expanded={expanded}>Collapse</Label>
        </button>
        {user && (
          <Menu
            align="start"
            trigger={
              <button type="button" data-tip={user.name} aria-label={`Account: ${user.name}`} className={cn(item, "mt-1 h-12 px-2")}>
                <Avatar name={user.name} hue={user.hue} size={24} />
                <Label expanded={expanded}>
                  <span className="block text-left text-body text-text">{user.name}</span>
                  <span className="block text-left text-small">{user.email}</span>
                </Label>
              </button>
            }
            items={accountItems}
          />
        )}
      </div>
      {node}
      {peek && peekAnchor && hydrated && createPortal(
        <div
          ref={peekPanel}
          data-open={peekOpen}
          inert={!peekOpen}
          onClick={closePeek}
          className={cn(
            "fixed z-50 w-[300px] origin-left rounded-card border border-border bg-surface p-3 shadow-[var(--shadow-float)]",
            "pointer-events-none -translate-x-1 scale-[0.97] opacity-0 transition-[opacity,transform,scale] duration-[var(--dur-2)] ease-out",
            "data-[open=true]:pointer-events-auto data-[open=true]:translate-x-0 data-[open=true]:scale-100 data-[open=true]:opacity-100",
          )}
          style={{ left: 64, top: Math.max(8, peekAnchor.top - 10) }}
        >
          {peek.content}
        </div>,
        document.body,
      )}
    </nav>
  );
}

/** Below 768px the sidebar becomes a bottom bar with labels. */
/** The bottom bar's count: pops when it goes up. */
function CountBadge({ count }: { count: number }) {
  const ref = useRef<HTMLSpanElement>(null);
  usePopOnIncrease(ref, count);
  return <span ref={ref} className="absolute top-1.5 left-1/2 ml-2 grid h-4 min-w-4 place-items-center rounded-full bg-text px-1 pt-px text-[10px] leading-none font-bold text-surface num">{count}</span>;
}

export function BottomBar({ items, pathname, onMore }: { items: NavItem[]; pathname: string; onMore: () => void }) {
  return (
    <nav aria-label="Main" className="fixed inset-x-0 bottom-0 z-30 flex border-t px-2 border-border bg-surface pb-[env(safe-area-inset-bottom)] md:hidden">
      {items.map((it) => {
        const active = isActive(pathname, it.href);
        return (
          <Link key={it.href} href={it.href} aria-current={active ? "page" : undefined}
            className={cn("relative flex h-14 flex-1 flex-col items-center justify-center gap-0.5 text-[11px]", active ? "text-text" : "text-muted")}>
            <it.icon className="size-5" strokeWidth={1.6} />
            {it.label}
            {it.count ? <CountBadge count={it.count} /> : null}
          </Link>
        );
      })}
      <button type="button" onClick={onMore} className="flex h-14 flex-1 flex-col items-center justify-center gap-0.5 text-[11px] text-muted">
        <Ellipsis aria-hidden className="size-5" strokeWidth={1.6} />
        More
      </button>
    </nav>
  );
}
