import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// Teach tailwind-merge our custom font-size tokens so `text-small` doesn't swallow `text-muted`.
const twMerge = extendTailwindMerge({
  extend: { classGroups: { "font-size": [{ text: ["small", "body", "section", "title", "kpi"] }] } },
});

export const cn = (...inputs: ClassValue[]) => twMerge(clsx(inputs));
