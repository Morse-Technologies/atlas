"use client";
import { useRef } from "react";
import { toast } from "sonner";
import { newKey, useInstallLicense } from "@/features/hooks";
import { Button, Spinner } from "@/ui/atoms";

/** "Install license file": reads the chosen file and sends its text. A refused file says why and changes nothing. */
export function LicenseInstall() {
  const input = useRef<HTMLInputElement>(null);
  const install = useInstallLicense();
  const send = async (file: File | undefined) => {
    if (!file) return;
    install.mutate({ content: await file.text(), key: newKey() }, { onSuccess: () => toast.success("License installed") });
    if (input.current) input.current.value = ""; // choosing the same file again must send it again
  };
  return (
    <div className="flex flex-col gap-2">
      <input ref={input} type="file" accept=".json,.lic" aria-label="License file" className="sr-only" tabIndex={-1} onChange={(e) => void send(e.target.files?.[0])} />
      <Button variant="outline" className="self-start" disabled={install.isPending} onClick={() => input.current?.click()}>
        {install.isPending && <Spinner />}Install license file
      </Button>
      {install.isError && <p role="alert" className="text-small text-negative">{install.error.message}</p>}
    </div>
  );
}
