import { Suspense } from "react";
import { AppShell } from "@/features/shell/AppShell";

// The shell reads the pathname, which only exists at request time; the ground colour holds the frame meanwhile.
export default function AppLayout({ children }: LayoutProps<"/">) {
  return (
    <Suspense fallback={<div className="min-h-dvh bg-ground" />}>
      <AppShell>{children}</AppShell>
    </Suspense>
  );
}
