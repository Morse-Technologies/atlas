"use client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Toaster } from "sonner";
import { SmoothScroll } from "@/lib/smooth";
import { TooltipProvider } from "@/ui/atoms";

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(
    // Stale-while-revalidate: cached data shows instantly and refreshes quietly on focus or after 30 s.
    // Failed writes retry with the same variables, so the same idempotency key.
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, refetchOnWindowFocus: true, retry: 1 }, mutations: { retry: 2 } } }),
  );
  return (
    <QueryClientProvider client={client}>
      <SmoothScroll />
      <TooltipProvider delay={400}>{children}</TooltipProvider>
      <Toaster
        position="bottom-right"
        toastOptions={{
          classNames: {
            toast: "!rounded-card !border-border !bg-surface !text-text !shadow-[var(--shadow-float)] !font-sans !text-body",
            description: "!text-muted",
          },
        }}
      />
    </QueryClientProvider>
  );
}
