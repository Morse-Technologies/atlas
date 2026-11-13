"use client";
import { useEffect, useRef } from "react";
import { Button, Spinner } from "@/ui/atoms";

type Props = { loaded: number; hasMore: boolean; loading: boolean; onLoadMore: () => void };

/** The end of a long list: loads the next page on its own when it scrolls into view, or when the button is pressed. */
export function LoadMore({ loaded, hasMore, loading, onLoadMore }: Props) {
  const end = useRef<HTMLDivElement>(null);
  const load = useRef(onLoadMore);
  useEffect(() => { load.current = onLoadMore; });
  useEffect(() => {
    const el = end.current;
    if (!el || !hasMore || loading || typeof IntersectionObserver === "undefined") return;
    const watcher = new IntersectionObserver((entries) => { if (entries.some((e) => e.isIntersecting)) load.current(); });
    watcher.observe(el);
    return () => watcher.disconnect();
  }, [hasMore, loading, loaded]);
  return (
    <div ref={end} className="flex flex-col items-center gap-2 py-4">
      <p role="status" aria-live="polite" className="text-small text-muted num">{loaded} loaded{hasMore ? "" : ", that is all"}</p>
      {hasMore && (
        <Button variant="outline" onClick={onLoadMore} disabled={loading}>
          {loading && <Spinner />}Load more
        </Button>
      )}
    </div>
  );
}
