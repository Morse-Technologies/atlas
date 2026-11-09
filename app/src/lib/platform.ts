"use client";
import { useSyncExternalStore } from "react";

const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
/** The command key for this machine: ⌘ on a Mac, Ctrl everywhere else (most office desktops are Windows). */
export const useMod = () => useSyncExternalStore(() => () => {}, () => (isMac() ? "⌘" : "Ctrl"), () => "⌘");
