"use client";
import { ChevronLeft, ChevronRight, Minus, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Document as PdfDocument, Page, pdfjs } from "react-pdf";
import "react-pdf/dist/Page/TextLayer.css";
import { cn } from "@/lib/cn";
import { Button, Skeleton, bandBg } from "@/ui/atoms";
import type { BandKind } from "@/lib/api/types";

pdfjs.GlobalWorkerOptions.workerSrc = new URL("pdfjs-dist/build/pdf.worker.min.mjs", import.meta.url).toString();

export type Highlight = { page: number; x: number; y: number; w: number; h: number; label: string; band?: BandKind };

/** A PDF page fitted to the column, with the cited passage marked in the accent. Points are PDF units, top-left origin. */
const NARROW_BELOW_PX = 640;
const PAGE_GUTTER_PX = 48;
const MAX_PAGE_PX = 900;
const MIN_PAGE_PX = 760;
const MIN_RENDER_PX = 240;

export default function PdfViewer({ src, initialPage = 1, highlight, pageWidthPts = 595 }: {
  src: string; initialPage?: number; highlight?: Highlight; pageWidthPts?: number;
}) {
  const [pages, setPages] = useState(0);
  const [page, setPage] = useState(initialPage);
  const [zoom, setZoom] = useState(1);
  const [width, setWidth] = useState(0);
  const [narrow, setNarrow] = useState(false); // phones: the page is panned, so no text layer (it would sit under the screen edge)
  const [sharp, setSharp] = useState<string>("");
  const box = useRef<HTMLDivElement>(null);
  const mark = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => { setNarrow(e.contentRect.width < NARROW_BELOW_PX); setWidth(Math.max(Math.min(e.contentRect.width - PAGE_GUTTER_PX, MAX_PAGE_PX), MIN_PAGE_PX)); });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // shortcut: pages never render narrower than 760px (readable text on phones, pan sideways); no fit-to-width on small screens.
  const renderWidth = Math.max(width * zoom, MIN_RENDER_PX);
  const scale = renderWidth / pageWidthPts;
  const showMark = highlight && highlight.page === page;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-12 flex-none items-center justify-between gap-3 border-b border-rule px-4">
        <div className="flex items-center gap-1">
          <Button size="icon" variant="ghost" aria-label="Previous page" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}><ChevronLeft /></Button>
          <span className="min-w-24 text-center text-body num" aria-live="polite">Page {page} of {pages || "…"}</span>
          <Button size="icon" variant="ghost" aria-label="Next page" disabled={!pages || page >= pages} onClick={() => setPage((p) => p + 1)}><ChevronRight /></Button>
        </div>
        <div className="flex items-center gap-2">
          {highlight && highlight.page !== page && (
            <Button size="sm" variant="outline" onClick={() => setPage(highlight.page)}>Back to the cited page</Button>
          )}
          <Button size="icon" variant="ghost" aria-label="Zoom out" disabled={zoom <= 0.6} onClick={() => setZoom((z) => +(z - 0.2).toFixed(1))}><Minus /></Button>
          <span className="w-12 text-center text-small text-muted num">{Math.round(zoom * 100)} %</span>
          <Button size="icon" variant="ghost" aria-label="Zoom in" disabled={zoom >= 2} onClick={() => setZoom((z) => +(z + 0.2).toFixed(1))}><Plus /></Button>
        </div>
      </div>
      <div ref={box} tabIndex={0} role="region" aria-label="Document pages" className="min-h-0 flex-1 overflow-auto bg-raised px-6 py-6">
        {width > 0 && (
          <PdfDocument
            file={src}
            onLoadSuccess={({ numPages }) => setPages(numPages)}
            loading={<Skeleton className="mx-auto aspect-[1/1.414] w-full max-w-[900px]" />}
            error={<p className="text-body text-negative">This file couldn&apos;t be opened. The original is still in its source folder.</p>}
          >
            <div className="relative mx-auto w-fit shadow-[0_1px_3px_rgb(0_0_0/0.08)]">
              {/* Blur-up placeholder: a 48px-wide render of the same page, scaled and blurred until the sharp one lands. */}
              <div aria-hidden className={`pointer-events-none absolute inset-0 z-10 overflow-hidden bg-surface transition-opacity duration-[var(--dur-3)] ease-out ${sharp === `${page}-${renderWidth}` ? "opacity-0" : "opacity-100"}`}>
                <div className="origin-top-left blur-md" style={{ transform: `scale(${renderWidth / 48})` }}>
                  <Page pageNumber={page} width={48} renderTextLayer={false} renderAnnotationLayer={false} loading={null} />
                </div>
              </div>
              <Page
                key={`${page}-${renderWidth}`}
                pageNumber={page}
                width={renderWidth}
                renderAnnotationLayer={false}
                renderTextLayer={!narrow}
                onRenderSuccess={() => {
                  setSharp(`${page}-${renderWidth}`);
                  // Scroll only the page pane so the cited passage sits a third of the way down.
                  if (showMark && mark.current && box.current) {
                    const top = mark.current.getBoundingClientRect().top - box.current.getBoundingClientRect().top + box.current.scrollTop - box.current.clientHeight / 3;
                    box.current.scrollTo({ top: Math.max(top, 0), behavior: "smooth" });
                  }
                }}
                loading={<Skeleton className="aspect-[1/1.414]" />}
              />
              {showMark && (
                <div
                  ref={mark}
                  role="note"
                  aria-label={highlight.label}
                  className={cn("pointer-events-none absolute rounded-[3px] bg-accent/12 ring-2 ring-accent animate-band-in")}
                  style={{ left: highlight.x * scale, top: highlight.y * scale, width: highlight.w * scale, height: highlight.h * scale }}
                >
                  <span className={cn("absolute top-0 left-full ml-3 hidden rounded-[3px] sm:block px-1.5 py-0.5 text-small font-bold whitespace-nowrap text-white", bandBg[highlight.band ?? "procedure"])}>
                    {highlight.label}
                  </span>
                </div>
              )}
            </div>
          </PdfDocument>
        )}
      </div>
    </div>
  );
}
