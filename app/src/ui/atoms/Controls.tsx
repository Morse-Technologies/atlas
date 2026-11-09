"use client";
import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Switch as BaseSwitch } from "@base-ui/react/switch";
import { Check } from "lucide-react";
import { cn } from "@/lib/cn";

type ToggleProps = {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  id?: string;
  "aria-label"?: string;
  "aria-describedby"?: string;
  className?: string;
};

export function Switch({ checked, onCheckedChange, className, ...rest }: ToggleProps) {
  return (
    <BaseSwitch.Root
      checked={checked}
      onCheckedChange={(v) => onCheckedChange(v)}
      className={cn(
        "relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full bg-control p-0.5",
        "transition-colors duration-[var(--dur-2)] ease-out data-[checked]:bg-primary",
        "data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60",
        className,
      )}
      {...rest}
    >
      <BaseSwitch.Thumb
        className={cn(
          "block size-4 rounded-full bg-surface shadow-[0_1px_2px_rgb(0_0_0/0.2)]",
          "transition-transform duration-[var(--dur-2)] ease-out data-[checked]:translate-x-4",
        )}
      />
    </BaseSwitch.Root>
  );
}

export function Checkbox({ checked, onCheckedChange, className, ...rest }: ToggleProps) {
  return (
    <BaseCheckbox.Root
      checked={checked}
      onCheckedChange={(v) => onCheckedChange(v)}
      className={cn(
        "flex size-[18px] shrink-0 items-center justify-center rounded-[4px] border-2 border-control bg-surface",
        "transition-colors duration-[var(--dur-1)] ease-out data-[checked]:border-primary data-[checked]:bg-primary",
        className,
      )}
      {...rest}
    >
      <BaseCheckbox.Indicator className="text-on-primary">
        <Check className="size-3" strokeWidth={3} />
      </BaseCheckbox.Indicator>
    </BaseCheckbox.Root>
  );
}
