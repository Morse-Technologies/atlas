"use client";
import { cva, type VariantProps } from "class-variance-authority";
import { forwardRef, useRef, type ButtonHTMLAttributes } from "react";
import { useMagnetic } from "@/lib/gsapIn";
import { cn } from "@/lib/cn";

export const buttonStyles = cva(
  [
    "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-control font-medium",
    "transition-[background-color,color,box-shadow,transform] duration-[var(--dur-1)] ease-out",
    "active:scale-[0.97] disabled:pointer-events-none disabled:opacity-60",
    "[&_svg]:size-4 [&_svg]:shrink-0",
  ],
  {
    variants: {
      variant: {
        primary: "bg-primary text-on-primary hover:bg-primary/88",
        secondary: "bg-active text-text hover:bg-[color-mix(in_srgb,var(--active),var(--text)_8%)]",
        outline: "border border-control bg-surface text-text hover:bg-raised",
        ghost: "text-text hover:bg-raised",
        danger: "border border-control bg-surface text-negative hover:border-negative focus-visible:border-negative",
      },
      size: {
        sm: "h-7 px-2.5 text-small",
        md: "h-8 px-3 text-body",
        lg: "h-10 px-4 text-body",
        icon: "hit-slop size-8",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof buttonStyles> & {
  /** Primary buttons lean toward the pointer; pass false to opt out (hold buttons do). */
  magnetic?: boolean;
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, type = "button", magnetic, children, ...props },
  ref,
) {
  const self = useRef<HTMLButtonElement | null>(null);
  const inner = useRef<HTMLSpanElement>(null);
  const pull = (magnetic ?? variant === "primary") && !props.disabled;
  useMagnetic(self, inner, pull);
  return (
    <button
      ref={(el) => { self.current = el; if (typeof ref === "function") ref(el); else if (ref) ref.current = el; }}
      type={type} className={cn(buttonStyles({ variant, size }), className)} {...props}
    >
      {variant === "primary" && magnetic !== false ? <span ref={inner} className="inline-flex items-center gap-2">{children}</span> : children}
    </button>
  );
});
