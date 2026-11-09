"use client";
import {
  FileText, FolderSync, LayoutDashboard, MessageSquareText, Settings, ShieldCheck, TriangleAlert, Users, Waypoints,
} from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { flushSync } from "react-dom";
import { useIsFetching } from "@tanstack/react-query";
import { Moon, PanelLeft, SquarePen, Sun } from "lucide-react";
import { ViewTransition, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useAlerts, useDocuments, useFeature, useMe, usePrefetchScreen, useSettings } from "@/features/hooks";
import { Kbd, Pill, Spinner } from "@/ui/atoms";
import { isDemoApi, type Alert, type Document } from "@/lib/api";
import { useMod } from "@/lib/platform";
import { hasScreen } from "@/lib/edition";
import { DocChip, SeverityPill, Sheet, docLabel } from "@/ui/cells";
import { AppSidebar, BottomBar, type NavItem } from "@/ui/organisms/AppRail";
import { CommandPalette, type PaletteItem } from "@/ui/organisms/CommandPalette";
import { ShellLayout, TopBar } from "@/ui/templates/ShellLayout";
import { useAccountMenu } from "./AccountMenu";
import { RenewalBanner } from "./RenewalBanner";
import Link from "next/link";
import { reduced } from "@/lib/motion";

const titles: Record<string, string> = {
  dashboard: "Dashboard", ask: "Assistant", documents: "Documents", alerts: "Alerts", map: "Map",
  sources: "Sources", audit: "Audit", users: "Users", settings: "Settings",
};

/** "G" then a letter jumps to a screen. */
const ALL_JUMPS: Record<string, string> = { d: "/dashboard", a: "/ask", o: "/documents", l: "/alerts", m: "/map", s: "/sources", u: "/users", t: "/audit", e: "/settings" };
const JUMPS = Object.fromEntries(Object.entries(ALL_JUMPS).filter(([, href]) => hasScreen(href)));
const BOTTOM_BAR_ITEMS = 4;
const G_CHORD_MS = 900;
const PALETTE_MAX_DOCS = 6; // time to press the second key of a "G then letter" jump
const HINTS: Record<string, string[]> = Object.fromEntries(Object.entries(JUMPS).map(([k, v]) => [v, ["G", k.toUpperCase()]]));

function AlertsPeek({ alerts, docs }: { alerts: Alert[]; docs: Document[] }) {
  const open = alerts.filter((a) => a.status === "open").sort((a, b) => ["high", "medium", "low"].indexOf(a.severity) - ["high", "medium", "low"].indexOf(b.severity));
  return (
    <div>
      <div className="flex items-baseline justify-between px-1 pb-2">
        <span className="text-body font-medium">Open alerts</span>
        <Link href="/alerts" className="text-small text-muted hover:text-text">View all {open.length}</Link>
      </div>
      <ul className="flex flex-col">
        {open.slice(0, 4).map((a) => {
          const d = docs.find((x) => x.id === a.documentIds[0]);
          return (
            <li key={a.id}>
              <Link href={`/alerts?id=${a.id}`} className="flex items-start gap-2 rounded-control px-1.5 py-2 hover:bg-raised">
                <span className="min-w-0 flex-1">
                  <span className="line-clamp-2 text-body">{a.title}</span>
                  {d && <DocChip doc={d} className="mt-1" />}
                </span>
                <SeverityPill severity={a.severity} />
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

type Theme = "light" | "dark";
const readTheme = (): Theme => {
  const attr = document.documentElement.dataset.theme;
  return attr === "dark" || (!attr && matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
};
const subscribeTheme = (cb: () => void) => {
  const mq = matchMedia("(prefers-color-scheme: dark)");
  window.addEventListener("atlas-theme", cb);
  mq.addEventListener("change", cb);
  return () => { window.removeEventListener("atlas-theme", cb); mq.removeEventListener("change", cb); };
};

function useTheme() {
  const theme = useSyncExternalStore(subscribeTheme, readTheme, () => "light" as Theme);
  /** The new theme is revealed as a circle growing from the click (View Transitions), like Skiper 26. */
  const toggle = useCallback((e?: { clientX: number; clientY: number }) => {
    const next: Theme = readTheme() === "dark" ? "light" : "dark";
    const apply = () => {
      document.documentElement.dataset.theme = next;
      try { localStorage.setItem("atlas-theme", next); } catch {}
      window.dispatchEvent(new Event("atlas-theme"));
    };
    const reduce = reduced();
    if (!document.startViewTransition || reduce) return apply();
    const x = e?.clientX ?? innerWidth / 2;
    const y = e?.clientY ?? innerHeight / 2;
    const r = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y));
    const vt = document.startViewTransition(() => flushSync(apply));
    vt.ready.then(() => {
      document.documentElement.animate(
        { clipPath: [`circle(0px at ${x}px ${y}px)`, `circle(${r}px at ${x}px ${y}px)`] },
        { duration: 560, easing: "cubic-bezier(0.77, 0, 0.175, 1)", pseudoElement: "::view-transition-new(root)" },
      );
    });
  }, []);
  return { theme, toggle };
}

/* Sidebar state, remembered per browser. */
const readNav = () => { try { return localStorage.getItem("atlas-nav") === "expanded"; } catch { return false; } };
const subscribeNav = (cb: () => void) => { window.addEventListener("atlas-nav", cb); return () => window.removeEventListener("atlas-nav", cb); };
function useSidebar() {
  const expanded = useSyncExternalStore(subscribeNav, readNav, () => false);
  const toggle = useCallback(() => {
    try { localStorage.setItem("atlas-nav", readNav() ? "collapsed" : "expanded"); } catch {}
    window.dispatchEvent(new Event("atlas-nav"));
  }, []);
  return { expanded, toggle };
}

/** ⌘K palette, ⌘\\ sidebar, and "G then a letter" jumps (ignored while typing). */
function useShellShortcuts({ onPalette, onToggleNav }: { onPalette: () => void; onToggleNav: () => void }) {
  const router = useRouter();
  const pendingG = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); onPalette(); return; }
      if ((e.metaKey || e.ctrlKey) && e.key === "\\") { e.preventDefault(); onToggleNav(); return; }
      const t = e.target as HTMLElement;
      if (e.metaKey || e.ctrlKey || e.altKey || t.closest("input, textarea, select, [contenteditable=true]")) return;
      if (pendingG.current) {
        clearTimeout(pendingG.current); pendingG.current = null;
        const to = JUMPS[e.key.toLowerCase()];
        if (to) { e.preventDefault(); router.push(to); }
        return;
      }
      if (e.key.toLowerCase() === "g") pendingG.current = setTimeout(() => { pendingG.current = null; }, G_CHORD_MS);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onPalette, onToggleNav, router]);
}

/** Sidebar groups for this edition and license: Pro screens are hidden in Core, locked without a license. */
function useNavGroups() {
  const { data: alerts } = useAlerts();
  const alertsPro = useFeature("alerts");
  const mapPro = useFeature("map");
  const openCount = alerts?.filter((a) => a.status === "open").length ?? 0;
  return useMemo<NavItem[][]>(() => [
    [
      { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
      { href: "/ask", label: "Assistant", icon: MessageSquareText },
      { href: "/documents", label: "Documents", icon: FileText },
    ],
    [
      { href: "/alerts", label: "Alerts", icon: TriangleAlert, count: alertsPro.enabled ? openCount : undefined, locked: !alertsPro.enabled && !alertsPro.isPending },
      { href: "/map", label: "Map", icon: Waypoints, locked: !mapPro.enabled && !mapPro.isPending },
    ],
    [
      { href: "/sources", label: "Sources", icon: FolderSync },
      { href: "/audit", label: "Audit", icon: ShieldCheck },
      { href: "/users", label: "Users", icon: Users },
    ],
  ].map((g) => g.filter((n) => hasScreen(n.href))).filter((g) => g.length), [openCount, alertsPro.enabled, alertsPro.isPending, mapPro.enabled, mapPro.isPending]);
}

type PaletteSource = { query: string; screens: NavItem[]; nav: ReturnType<typeof useSidebar>; theme: ReturnType<typeof useTheme> };

/** Palette rows: matching documents first, then screens, then actions. */
function usePaletteItems({ query, screens, nav, theme }: PaletteSource): PaletteItem[] {
  const router = useRouter();
  const { data: docs } = useDocuments({ q: query });
  return useMemo(() => {
    const q = query.trim().toLowerCase();
    const matches = (label: string) => !q || label.toLowerCase().includes(q);
    const goTo = screens.filter((n) => matches(n.label)).map<PaletteItem>((n) => ({
      id: `go-${n.href}`, group: "Go to", label: n.label, icon: <n.icon />, onSelect: () => router.push(n.href),
      hint: HINTS[n.href] ? <span className="flex gap-0.5">{HINTS[n.href].map((k) => <Kbd key={k}>{k}</Kbd>)}</span> : undefined,
    }));
    const actions: PaletteItem[] = [
      { id: "act-new", group: "Actions", label: "New chat", icon: <SquarePen />, onSelect: () => router.push("/ask"), hint: <span className="flex gap-0.5"><Kbd>⌘</Kbd><Kbd>⇧</Kbd><Kbd>O</Kbd></span> },
      { id: "act-nav", group: "Actions", label: nav.expanded ? "Collapse sidebar" : "Expand sidebar", icon: <PanelLeft />, onSelect: nav.toggle, hint: <span className="flex gap-0.5"><Kbd>⌘</Kbd><Kbd>\\</Kbd></span> },
      { id: "act-theme", group: "Actions", label: theme.theme === "dark" ? "Switch to light theme" : "Switch to dark theme", icon: theme.theme === "dark" ? <Sun /> : <Moon />, onSelect: () => theme.toggle() },
    ].filter((a) => matches(a.label));
    const documents = (q ? docs ?? [] : []).slice(0, PALETTE_MAX_DOCS).map<PaletteItem>((d) => ({
      id: `doc-${d.id}`, group: "Documents", label: `${docLabel(d)}, ${d.title}`, hint: d.department,
      icon: <FileText />, onSelect: () => router.push(`/documents/${d.id}`),
    }));
    return [...documents, ...goTo, ...actions];
  }, [query, docs, screens, nav.expanded, nav.toggle, theme.theme, theme.toggle, router]); // eslint-disable-line react-hooks/exhaustive-deps
}

export function AppShell({ children }: { children: ReactNode }) {
  const mod = useMod();
  const pathname = usePathname();
  const router = useRouter();
  const { data: me } = useMe();
  const orgName = useSettings().data?.orgName ?? "";
  const { data: alerts } = useAlerts();
  const alertsPro = useFeature("alerts");
  const themeState = useTheme();
  const { theme, toggle } = themeState;
  const nav = useSidebar();
  const [palette, setPalette] = useState(false);
  const [more, setMore] = useState(false);
  const [query, setQuery] = useState("");
  const { data: allDocs } = useDocuments({});
  const prefetch = usePrefetchScreen();
  const fetching = useIsFetching();
  const account = useAccountMenu();
  const togglePalette = useCallback(() => setPalette((o) => !o), []);
  useShellShortcuts({ onPalette: togglePalette, onToggleNav: nav.toggle });

  const groups = useNavGroups();
  const footer: NavItem[] = [{ href: "/settings", label: "Settings", icon: Settings }];
  const all = useMemo(() => [...groups.flat(), ...footer], [groups]); // eslint-disable-line react-hooks/exhaustive-deps
  const paletteItems = usePaletteItems({ query, screens: all, nav, theme: themeState });

  const section = titles[pathname.split("/")[1] ?? ""] ?? "";

  return (
    <ShellLayout
      rail={
        <AppSidebar
          groups={groups} footer={footer} pathname={pathname} user={me ?? undefined}
          expanded={nav.expanded} onToggleExpanded={nav.toggle} onIntent={prefetch}
          peek={alertsPro.enabled && hasScreen("/alerts") ? { href: "/alerts", content: <AlertsPeek alerts={alerts ?? []} docs={allDocs ?? []} /> } : undefined}
          theme={theme} onToggleTheme={(e) => toggle(e)} accountItems={account.items}
        />
      }
      topbar={
        <TopBar scope={<><span className="truncate text-text">{orgName}</span><span aria-hidden>/</span><span className="truncate">{section}</span>{isDemoApi && <Pill className="ml-auto sm:ml-2">Demo data</Pill>}</>}>
          <span role="status" aria-live="polite" className={`inline-flex items-center gap-1.5 text-small text-muted transition-opacity duration-[var(--dur-2)] ${fetching ? "opacity-100" : "opacity-0"}`}>
            <Spinner className="size-3" />{fetching ? "Updating" : ""}
          </span>
          <button
            type="button"
            onClick={() => setPalette(true)}
            className="hidden h-8 w-72 items-center justify-between rounded-control border border-control bg-surface px-3 text-body text-faint transition-colors duration-[var(--dur-1)] hover:border-muted lg:flex"
          >
            Search or ask
            <span className="text-small">{mod === "⌘" ? "⌘K" : "Ctrl K"}</span>
          </button>
        </TopBar>
      }
      bottomBar={<BottomBar items={all.slice(0, BOTTOM_BAR_ITEMS)} pathname={pathname} onMore={() => setMore(true)} />}
    >
      <ViewTransition key={pathname.split("/")[1]} enter="page-enter" exit="page-exit" default="none">
        <div className="flex min-h-0 flex-1 flex-col"><RenewalBanner />{children}</div>
      </ViewTransition>
      <CommandPalette
        open={palette} onOpenChange={(o) => { setPalette(o); if (!o) setQuery(""); }}
        query={query} onQueryChange={setQuery} items={paletteItems}
        onAsk={(q) => router.push(`/ask?q=${encodeURIComponent(q)}`)}
      />
      {account.sheet}
      <Sheet open={more} onOpenChange={setMore} title="Atlas" description={orgName}>
        <nav aria-label="All screens" className="flex flex-col p-2">
          {all.map((n) => (
            <Link key={n.href} href={n.href} onClick={() => setMore(false)} className="flex h-11 items-center gap-3 rounded-control px-3 text-body hover:bg-raised">
              <n.icon className="size-[18px] text-muted" strokeWidth={1.6} />{n.label}
            </Link>
          ))}
          <button type="button" onClick={(e) => toggle(e)} className="flex h-11 items-center gap-3 rounded-control px-3 text-left text-body hover:bg-raised">
            {theme === "dark" ? "Light theme" : "Dark theme"}
          </button>
        </nav>
      </Sheet>
    </ShellLayout>
  );
}
