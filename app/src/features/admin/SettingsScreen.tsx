"use client";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, undoable, useLicense, useSettings, useUpdateSettings } from "@/features/hooks";
import { isDemoApi, setPreviewPlan } from "@/lib/api";
import type { SettingsPatch } from "@/lib/api/types";
import { fmtDate } from "@/lib/format";
import { Button, Input, Pill, Select, Switch } from "@/ui/atoms";
import { ErrorState, Field, KeyValues, LoadingRows, PageHeader, Segmented } from "@/ui/cells";
import { SettingsSection, SwitchRow, fmtWhen } from "@/ui/organisms/Admin";
import { Canvas } from "@/ui/templates/ShellLayout";

/* Screen 10: Settings ------------------------------------------------------------------------------ */
export function SettingsScreen() {
  const { data: s, isPending, isError, refetch } = useSettings();
  const { data: license } = useLicense();
  const update = useUpdateSettings();
  const qc = useQueryClient();
  const [orgName, setOrgName] = useState<string | null>(null);

  const save = (patch: SettingsPatch, what: string) => {
    const prev = s ? (Object.fromEntries(Object.keys(patch).map((k) => [k, s[k as keyof SettingsPatch]])) as SettingsPatch) : null;
    void undoable({
      pending: `Saving ${what.toLowerCase()}…`, done: `${what} saved`,
      run: () => update.mutateAsync({ patch, key: newKey() }),
      undo: prev ? () => update.mutateAsync({ patch: prev, key: newKey() }) : undefined,
    }).catch(() => {});
  };
  const preview = (plan: "core" | "pro") => { setPreviewPlan(plan); qc.invalidateQueries({ queryKey: ["license"] }); toast(plan === "core" ? "Previewing Atlas Core" : "Back to Atlas Pro"); };

  if (isError) return <Canvas><ErrorState what="settings" onRetry={() => refetch()} /></Canvas>;
  return (
    <Canvas className="max-w-[1152px]">
      <PageHeader title="Settings" subline="Changes save on their own and are recorded in the audit log." />
      {isPending || !s ? <div className="mt-8"><LoadingRows rows={8} pitch="h-14" /></div> : (
        <div className="mt-6">
          <SettingsSection title="Organization" description="The name people see at the top of every screen.">
            <Field label="Organization name">
              {({ id }) => (
                <div className="flex gap-2">
                  <Input id={id} value={orgName ?? s.orgName} onChange={(e) => setOrgName(e.target.value)} className="bg-surface" />
                  <Button variant="outline" size="lg" disabled={orgName === null || orgName.trim() === "" || orgName === s.orgName}
                    onClick={() => { save({ orgName: orgName!.trim() }, "Organization name"); setOrgName(null); }}>Save</Button>
                </div>
              )}
            </Field>
          </SettingsSection>

          <SettingsSection title="Answers" description="How Atlas behaves when it answers. These are the promises the assistant keeps."
            aside={<div className="mt-4 rounded-card border border-border p-3 text-small"><p className="text-muted">With these settings, a weak match reads:</p><p className="mt-1">“I couldn&apos;t find this in your documents. These might be related: …”</p></div>}>
            <SwitchRow title="Always cite a source" badge={<Pill tone={s.requireCitation ? "positive" : "warning"}>{s.requireCitation ? "On" : "Off"}</Pill>}
              description="Atlas refuses to answer when no passage supports the claim. Turning this off is not recommended."
              control={<Switch aria-label="Always cite a source" checked={s.requireCitation} onCheckedChange={(v) => save({ requireCitation: v }, "Citation rule")} />} />
            <SwitchRow title="Warn about contradictions" description="When two current documents disagree, the answer says so and names both."
              control={<Switch aria-label="Warn about contradictions" checked={s.warnContradictions} onCheckedChange={(v) => save({ warnContradictions: v }, "Contradiction warning")} />} />
            <Field label={`Relevance needed to answer: ${s.notFoundThreshold.toFixed(2)}`} help="Higher means more “not found” and fewer weak answers. Calibrated with your organization's evaluation set; 0.35 is the starting point.">
              {({ id, describedBy }) => (
                <input id={id} aria-describedby={describedBy} type="range" min={0.2} max={0.6} step={0.05} value={s.notFoundThreshold}
                  onChange={(e) => save({ notFoundThreshold: Number(e.target.value) }, "Threshold")} className="w-full accent-[var(--text)]" />
              )}
            </Field>
            <div className="flex flex-col gap-2">
              <span className="text-body font-medium" id="lang-label">Answer in</span>
              <Segmented className="max-w-full self-start overflow-x-auto" label="Answer in" value={s.answerLanguage} onChange={(v) => save({ answerLanguage: v }, "Answer language")}
                options={[{ value: "user", label: "Same as the question" }, { value: "es", label: "Spanish" }, { value: "en", label: "English" }]} />
            </div>
          </SettingsSection>

          <SettingsSection title="Models and network" description="Everything runs on this server. Change models in the deployment, then check the evaluation report.">
            <KeyValues rows={[{ k: "Answers", v: s.chatModel }, { k: "Search", v: s.embedModel }, { k: "Reranking", v: s.rerankModel }]} />
            <SwitchRow title="Network isolated" description="Atlas makes no outbound connections: no telemetry, no license checks, no model downloads. Set in the deployment."
              control={<Pill tone={s.networkIsolated ? "positive" : "neutral"}>{s.networkIsolated ? "On" : "Off"}</Pill>} />
          </SettingsSection>

          <SettingsSection title="Retention" description="How long questions, answers and audit events are kept. Documents stay as long as they exist in their source.">
            <Field label="Keep records for">
              {({ id }) => (
                <Select id={id} value={s.retentionDays} onChange={(e) => save({ retentionDays: Number(e.target.value) }, "Retention")} className="h-10 bg-surface">
                  <option value={365}>1 year</option><option value={730}>2 years</option><option value={1825}>5 years</option>
                </Select>
              )}
            </Field>
          </SettingsSection>

          <SettingsSection title="License" description="Checked offline with a signed file. Nothing is sent to Morse.">
            {license ? (
              <>
                <KeyValues rows={[
                  { k: "Plan", v: license.plan === "pro" ? "Atlas Pro" : "Atlas Core (open source)" },
                  { k: "Organization", v: license.organization },
                  { k: "Valid until", v: license.plan === "pro" ? fmtDate(license.validUntil) : "n/a" },
                ]} />
                {isDemoApi && (
                  <div className="rounded-card border border-dashed border-control p-3">
                    <SwitchRow title="Preview as Atlas Core" badge={<Pill>Demo only</Pill>} description="Shows how the app looks without a Pro license. Resets on reload."
                      control={<Switch aria-label="Preview as Atlas Core" checked={license.plan === "core"} onCheckedChange={(v) => preview(v ? "core" : "pro")} />} />
                  </div>
                )}
              </>
            ) : <LoadingRows rows={3} pitch="h-8" />}
          </SettingsSection>
          <p className="pb-4 text-small text-muted" suppressHydrationWarning>Last change recorded {update.data ? fmtWhen(new Date().toISOString()) : "before this session"}.</p>
        </div>
      )}
    </Canvas>
  );
}
