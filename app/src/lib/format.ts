import { clockNow } from "@/lib/clock";

const TZ = "Europe/Madrid";
const date = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: TZ });
const dateShort = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", timeZone: TZ });
const time = new Intl.DateTimeFormat("en-GB", { hour: "2-digit", minute: "2-digit", timeZone: TZ });
const weekday = new Intl.DateTimeFormat("en-GB", { weekday: "short", timeZone: TZ });
const num = new Intl.NumberFormat("en-GB");

export const fmtDate = (iso: string | null) => (iso ? date.format(new Date(iso)).replace("Sept", "Sep") : "n/a");
export const fmtDayMonth = (iso: string) => dateShort.format(new Date(iso)).replace("Sept", "Sep");
export const fmtTime = (iso: string) => time.format(new Date(iso));
export const fmtWeekday = (iso: string) => weekday.format(new Date(iso));
export const fmtNum = (n: number) => num.format(n);
export const fmtSeconds = (ms: number) => `${(ms / 1000).toFixed(1)} s`;

/** Time in words for recency: "just now", "4 min ago", "Yesterday", "6 days ago". */
export function fmtAgo(iso: string | null): string {
  if (!iso) return "never";
  const diff = clockNow().getTime() - new Date(iso).getTime();
  const min = Math.round(diff / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} min ago`;
  const h = Math.round(min / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  if (d === 1) return "Yesterday";
  if (d < 30) return `${d} days ago`;
  return fmtDate(iso);
}

const MS_PER_DAY = 86_400_000;

/** Whole days from today to a date (negative = in the past). */
export const daysUntil = (isoDate: string) =>
  Math.round((new Date(isoDate).getTime() - clockNow().getTime()) / MS_PER_DAY);

/** Loose shape check (something@domain.tld); the server sends the real confirmation. */
export const isEmail = (s: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);

export const plural = (n: number, one: string, many = `${one}s`) => `${fmtNum(n)} ${n === 1 ? one : many}`;
