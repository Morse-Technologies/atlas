"use client";
import {
  forwardRef, useCallback, useEffect, useImperativeHandle, useRef,
  type InputHTMLAttributes, type SelectHTMLAttributes, type TextareaHTMLAttributes,
} from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/cn";

const field =
  "w-full rounded-control border border-control bg-surface text-body text-text " +
  "transition-[border-color,box-shadow] duration-[var(--dur-1)] ease-out " +
  "hover:border-[color-mix(in_srgb,var(--control),var(--text)_20%)] " +
  "focus-visible:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/25 " +
  "aria-invalid:border-negative disabled:bg-raised disabled:text-muted";

let measureCtx: CanvasRenderingContext2D | null = null;
const measure = (text: string, style: CSSStyleDeclaration) => {
  measureCtx ??= document.createElement("canvas").getContext("2d");
  if (!measureCtx) return 0;
  measureCtx.font = `${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
  measureCtx.letterSpacing = style.letterSpacing === "normal" ? "0px" : style.letterSpacing;
  return measureCtx.measureText(text).width;
};

/**
 * Single-line input with a smooth caret (after Skiper 106): the native caret is hidden and a teal caret
 * glides to each new position. Selections and disabled fields fall back to the native behaviour.
 */
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input(
  { className, type = "text", onBlur, onFocus, ...props },
  ref,
) {
  const inner = useRef<HTMLInputElement>(null);
  const caret = useRef<HTMLSpanElement>(null);
  const typingTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useImperativeHandle(ref, () => inner.current!);
  // Email inputs expose no selection API, so they render as text with an email keyboard.
  const htmlType = type === "email" ? "text" : type;

  const update = useCallback(() => {
    const el = inner.current, c = caret.current;
    if (!el || !c) return;
    const focused = document.activeElement === el;
    const start = el.selectionStart ?? el.value.length;
    const collapsed = start === (el.selectionEnd ?? start);
    if (!focused || !collapsed || el.disabled || el.readOnly) { c.dataset.on = "false"; return; }
    const s = getComputedStyle(el);
    const before = type === "password" ? "•".repeat(start) : el.value.slice(0, start);
    const x = parseFloat(s.paddingLeft) + parseFloat(s.borderLeftWidth) + measure(before, s) - el.scrollLeft;
    c.style.transform = `translate3d(${x}px, -50%, 0)`;
    c.dataset.on = "true";
    c.dataset.typing = "true";
    clearTimeout(typingTimer.current);
    typingTimer.current = setTimeout(() => { if (caret.current) caret.current.dataset.typing = "false"; }, 500);
  }, [type]);

  useEffect(() => {
    const onSel = () => { if (document.activeElement === inner.current) update(); };
    document.addEventListener("selectionchange", onSel);
    const ro = new ResizeObserver(update);
    if (inner.current) ro.observe(inner.current);
    return () => { document.removeEventListener("selectionchange", onSel); ro.disconnect(); clearTimeout(typingTimer.current); };
  }, [update]);

  return (
    <span className="relative flex w-full">
      <input
        ref={inner}
        type={htmlType}
        inputMode={type === "email" ? "email" : props.inputMode}
        className={cn(field, "h-10 px-3 caret-transparent", className)}
        onInput={update}
        onKeyUp={update}
        onScroll={update}
        onFocus={(e) => { onFocus?.(e); requestAnimationFrame(update); }}
        onBlur={(e) => { onBlur?.(e); update(); }}
        {...props}
      />
      <span
        ref={caret}
        aria-hidden
        data-on="false"
        className={cn(
          "pointer-events-none absolute top-1/2 left-0 h-[1.15em] w-[2px] rounded-full bg-accent text-body opacity-0",
          "transition-transform duration-[110ms] ease-out motion-reduce:transition-none",
          "data-[on=true]:animate-[caret-blink_1.06s_steps(1)_infinite] data-[on=true]:opacity-100 data-[typing=true]:animate-none",
        )}
      />
    </span>
  );
});

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea(
  { className, ...props },
  ref,
) {
  return <textarea ref={ref} className={cn(field, "min-h-20 resize-none px-3 py-2.5 leading-6", className)} {...props} />;
});

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select(
  { className, children, ...props },
  ref,
) {
  return (
    <span className="relative inline-flex w-full">
      <select ref={ref} className={cn(field, "h-8 appearance-none pr-8 pl-3", className)} {...props}>
        {children}
      </select>
      <ChevronDown aria-hidden className="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted" />
    </span>
  );
});
