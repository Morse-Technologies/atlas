"use client";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { OTPField } from "@base-ui/react/otp-field";
import { ApiError, DEMO_ACCOUNT, api, isDemoApi } from "@/lib/api";
import { safeNext } from "@/lib/auth";
import { ArrowRight, Check, Copy, Eye, EyeOff, FolderOpen, LogIn, Upload } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { newKey } from "@/features/hooks";
import { isEmail } from "@/lib/format";
import { Button, Input, Spinner } from "@/ui/atoms";
import { Field, Segmented, Steps } from "@/ui/cells";
import type { SetupInput } from "@/lib/api";
import { AuthLayout } from "@/ui/templates/AuthLayout";

/** The line under the mark: what Atlas promises. */
const CODE_LENGTH = 6;

const Caption = <p>Every answer points to the page it came from: <span className="font-medium">document, revision, section, page.</span></p>;

/* Sign in: password, then a one-time code -------------------------------------------------------- */
export function LoginScreen() {
  const router = useRouter();
  const next = useSearchParams().get("next");
  const [step, setStep] = useState<"password" | "code">("password");
  const [challengeId, setChallengeId] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [show, setShow] = useState(false);
  const [caps, setCaps] = useState(false);

  const submitPassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim() || !password) { setError("Enter your email and password."); return; }
    setBusy(true); setError(null);
    try {
      const challenge = await api.login(email.trim(), password);
      setChallengeId(challenge.challengeId);
      if (challenge.needsCode) setStep("code");
      else await finishSignIn(challenge.challengeId);
    } catch (err) {
      setError(signInMessage(err, "That email and password don't match. Check them and try again."));
    } finally {
      setBusy(false);
    }
  };

  /** Completes sign-in. The server sets the session cookie; the app then loads the page it was heading to. */
  const finishSignIn = async (id: string, value?: string) => {
    await api.verifyLogin(id, value);
    router.replace(safeNext(next));
  };

  const submitCode = async (value: string) => {
    setBusy(true); setError(null);
    try {
      await finishSignIn(challengeId, value);
    } catch (err) {
      setBusy(false); setCode("");
      setError(signInMessage(err, "That code isn't right. Check the latest one and try again."));
    }
  };

  return (
    <AuthLayout aside={Caption}>
      <div key={step} className="animate-enter">
        {step === "password" ? (
          <>
            <h1 className="text-title font-medium">Sign in to Atlas</h1>
            <p className="mt-1 text-body text-muted">Atlas only shows answers from documents you can already open.</p>
            <form onSubmit={submitPassword} className="mt-7 flex flex-col gap-4" noValidate>
              <Field label="Email">{({ id }) => <Input id={id} type="email" autoComplete="username" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}</Field>
              <Field label="Password" error={error ?? undefined} help={caps ? "Caps Lock is on." : undefined}>{({ id, describedBy, invalid }) => (
                <div className="relative">
                  <Input id={id} type={show ? "text" : "password"} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)}
                    onKeyUp={(e) => setCaps(e.getModifierState("CapsLock"))} onBlur={() => setCaps(false)} className="pr-11" aria-describedby={describedBy} aria-invalid={invalid} />
                  <button type="button" onClick={() => setShow((v) => !v)} aria-pressed={show} aria-label={show ? "Hide password" : "Show password"}
                    className="hit-slop absolute top-1/2 right-1.5 grid size-8 -translate-y-1/2 place-items-center rounded-full text-muted hover:bg-raised hover:text-text">
                    {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </button>
                </div>
              )}</Field>
              <Button type="submit" variant="primary" size="lg" className="mt-2" disabled={busy}>
                {busy ? <Spinner className="text-on-primary" /> : <LogIn />}Continue
              </Button>
            </form>
          </>
        ) : (
          <>
            <h1 className="text-title font-medium">Enter your code</h1>
            <p className="mt-1 text-body text-muted">Open the authenticator for {email.trim().toLowerCase()} and enter the 6-digit code.</p>
            <form onSubmit={(e) => { e.preventDefault(); if (code.length === 6) submitCode(code); }} className="mt-7 flex flex-col gap-3">
              <label htmlFor="otp" className="text-body font-medium">Verification code</label>
              <CodeInput value={code} onChange={setCode} onComplete={submitCode} />
              <p id="otp-help" className={error ? "text-small text-negative" : "text-small text-muted"}>{error ?? "The code changes every 30 seconds."}</p>
              <div className="mt-2 flex items-center gap-2">
                <Button type="submit" variant="primary" size="lg" className="flex-1" disabled={busy || code.length < 6}>
                  {busy ? <Spinner className="text-on-primary" /> : <LogIn />}Sign in
                </Button>
                <Button variant="ghost" size="lg" onClick={() => { setStep("password"); setCode(""); setError(null); }}>Back</Button>
              </div>
            </form>
          </>
        )}
      </div>
      {isDemoApi && step === "password" && (
        <button type="button" onClick={() => { setEmail(DEMO_ACCOUNT.email); setPassword(DEMO_ACCOUNT.password); setError(null); }}
          className="mt-5 flex w-full items-center justify-between gap-3 rounded-card border border-dashed border-control px-3.5 py-2.5 text-left text-small text-muted transition-[border-color,color] duration-[var(--dur-1)] hover:border-accent hover:text-text">
          <span>Demo: fill in the demo account <span className="text-text">{DEMO_ACCOUNT.email}</span></span>
          <ArrowRight className="size-4 flex-none" />
        </button>
      )}
      {isDemoApi && step === "code" && <p className="mt-5 rounded-card bg-raised px-3.5 py-2.5 text-small text-muted">Demo code: <span className="text-text num">{DEMO_ACCOUNT.code}</span></p>}
      <p className="mt-8 text-body text-muted">First time on this server? <Link href="/setup" className="text-text underline">Set up Atlas</Link></p>
    </AuthLayout>
  );
}

/** A wrong password, an unknown email and a locked account all answer 401 alike; anything else is a fault. */
function signInMessage(err: unknown, wrongCredentials: string): string {
  if (err instanceof ApiError && err.status === 401) return wrongCredentials;
  if (err instanceof ApiError && err.status < 500) return err.message;
  if (err instanceof Error && err.message === "Invalid credentials") return wrongCredentials; // the demo adapter
  return "Atlas couldn't be reached. Try again in a moment.";
}

/** Six boxes for the one-time code. Calls onComplete as soon as the last digit is typed. */
function CodeInput({ value, onChange, onComplete }: { value: string; onChange: (v: string) => void; onComplete: (v: string) => void }) {
  return (
    <OTPField.Root id="otp" length={CODE_LENGTH} value={value} onValueChange={onChange} onValueComplete={onComplete}
      autoComplete="one-time-code" inputMode="numeric" aria-describedby="otp-help" className="flex gap-2">
      {Array.from({ length: CODE_LENGTH }, (_, i) => (
        <OTPField.Input key={i} autoFocus={i === 0} aria-label={i === 0 ? undefined : `Digit ${i + 1} of ${CODE_LENGTH}`}
          className="h-12 w-11 rounded-control border border-control bg-surface text-center text-section font-medium text-text caret-accent num transition-[border-color,box-shadow,transform] duration-[var(--dur-1)] ease-out focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none data-[filled]:scale-[1.03]" />
      ))}
    </OTPField.Root>
  );
}

/* Setup wizard: administrator → second factor → first source ------------------------------------- */
const STEPS = ["Account", "Sign-in", "Source"];
const MIN_PASSWORD_LEN = 12;
const FOLDER_PATH = /^(\\\\[^\\]+\\[^\\]+.*|\/.+)$/; // \\server\share\… or an absolute path

type SetupForm = {
  account: SetupInput;
  kind: "folder" | "upload";
  folder: string;
  check: "idle" | "checking" | "ok" | "failed";
  checkDetail: string;
  totpUri: string;
};
type StepProps = { form: SetupForm; set: (patch: Partial<SetupForm>) => void };

const EMPTY_FORM: SetupForm = { account: { orgName: "", name: "", email: "", password: "" }, kind: "folder", folder: "", check: "idle", checkDetail: "", totpUri: "" };
const accountOk = ({ orgName, name, email, password }: SetupInput) =>
  !!orgName.trim() && !!name.trim() && isEmail(email) && password.length >= MIN_PASSWORD_LEN;
const folderName = (path: string) => path.trim().split(/[\\/]/).filter(Boolean).pop() ?? "Documents";

/** True once an administrator exists: the wizard must not offer to create a second one. */
function useSetupClosed() {
  const [closed, setClosed] = useState(false);
  useEffect(() => { api.setupStatus().then((s) => setClosed(s.setupDone), () => undefined); }, []);
  return closed;
}

export function SetupScreen() {
  const router = useRouter();
  const [step, setStep] = useState(0);
  const [form, setForm] = useState(EMPTY_FORM);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const closed = useSetupClosed() && step === 0;
  const idempotency = useRef<string>(undefined); // one key per attempt to create the administrator, so a retry cannot create two
  const set = (patch: Partial<SetupForm>) => setForm((f) => ({ ...f, ...patch }));

  const run = async (work: () => Promise<void>) => {
    setBusy(true); setError(null);
    try { await work(); } catch (err) { setError(setupMessage(err)); } finally { setBusy(false); }
  };
  const createAdmin = () => run(async () => {
    idempotency.current ??= newKey(); // made on click, not while rendering: the key is random
    const { totpUri } = await api.setup(form.account, idempotency.current);
    set({ totpUri }); setStep(1);
  });
  const signIn = (code: string) => run(async () => {
    const challenge = await api.login(form.account.email, form.account.password);
    await api.verifyLogin(challenge.challengeId, challenge.needsCode ? code : undefined);
    setStep(2);
  });
  const finish = () => run(async () => {
    if (form.kind === "folder") await api.createSource({ name: folderName(form.folder), kind: "folder", location: form.folder.trim() });
    toast.success("Atlas is indexing your first source", { description: "You can ask as soon as the first documents are in. Progress is on Sources." });
    router.push("/sources");
  });

  if (closed) return <AlreadySetUp />;
  return (
    <AuthLayout aside={Caption}>
      <Steps steps={STEPS} current={step} />
      <div key={step} className="mt-8 animate-enter">
        {step === 0 && <AccountStep form={form} set={set} />}
        {step === 1 && <SecondFactorStep form={form} busy={busy} onCode={signIn} />}
        {step === 2 && <SourceStep form={form} set={set} />}
      </div>
      {error && <p role="alert" className="mt-4 text-small text-negative">{error}</p>}
      <div className="mt-8 flex items-center justify-between gap-3">
        {step === 0 ? <Link href="/login" className="text-body text-muted hover:text-text">I already have an account</Link> : <span />}
        {step === 0 && <Button variant="primary" size="lg" disabled={!accountOk(form.account) || busy} onClick={createAdmin}>{busy ? <Spinner className="text-on-primary" /> : null}Continue<ArrowRight /></Button>}
        {step === 2 && <Button variant="primary" size="lg" disabled={busy || (form.kind === "folder" && form.check !== "ok")} onClick={finish}>{busy ? <Spinner className="text-on-primary" /> : null}Start indexing</Button>}
      </div>
    </AuthLayout>
  );
}

function AlreadySetUp() {
  return (
    <AuthLayout aside={Caption}>
      <h1 className="text-title font-medium">Atlas is already set up</h1>
      <p className="mt-1 text-body text-muted">An administrator exists. Ask them to add you, or sign in.</p>
      <Link href="/login" className="mt-6 inline-block text-body text-text underline">Go to sign in</Link>
    </AuthLayout>
  );
}

/** What the server says when creating the administrator or the first source fails. */
function setupMessage(err: unknown): string {
  if (err instanceof ApiError && err.status === 409) return "Atlas is already set up. Sign in instead.";
  if (err instanceof ApiError && err.status === 401) return "That code isn't right. Check the latest one and try again.";
  if (err instanceof ApiError && err.status < 500) return err.message;
  if (err instanceof Error && err.message === "Invalid credentials") return "That code isn't right. Check the latest one and try again.";
  return "Atlas couldn't be reached. Try again in a moment.";
}

function StepTitle({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <h1 className="text-title font-medium">{title}</h1>
      <p className="mt-1 text-body text-muted">{children}</p>
    </>
  );
}

function AccountStep({ form: { account }, set }: StepProps) {
  const edit = (patch: Partial<SetupInput>) => set({ account: { ...account, ...patch } });
  const short = account.password && account.password.length < MIN_PASSWORD_LEN;
  return (
    <>
      <StepTitle title="Create the administrator account">You can connect company sign-in later in Settings.</StepTitle>
      <div className="mt-6 flex flex-col gap-4">
        <Field label="Organization">{({ id }) => <Input id={id} value={account.orgName} onChange={(e) => edit({ orgName: e.target.value })} autoComplete="organization" />}</Field>
        <Field label="Full name">{({ id }) => <Input id={id} value={account.name} onChange={(e) => edit({ name: e.target.value })} autoComplete="name" />}</Field>
        <Field label="Work email">{({ id }) => <Input id={id} type="email" value={account.email} onChange={(e) => edit({ email: e.target.value })} autoComplete="email" />}</Field>
        <Field label="Password" help={`At least ${MIN_PASSWORD_LEN} characters.`} error={short ? `${MIN_PASSWORD_LEN - account.password.length} more characters.` : undefined}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" value={account.password} onChange={(e) => edit({ password: e.target.value })} autoComplete="new-password" aria-describedby={describedBy} aria-invalid={invalid} />}
        </Field>
      </div>
    </>
  );
}

/** Shows the secret to put in an authenticator app, then asks for its first code, which also signs the administrator in. */
function SecondFactorStep({ form, busy, onCode }: { form: SetupForm; busy: boolean; onCode: (code: string) => void }) {
  const [code, setCode] = useState("");
  const secret = secretOf(form.totpUri);
  return (
    <>
      <StepTitle title="Add the second factor">Add this key to an authenticator app on your phone, then enter the 6-digit code it shows.</StepTitle>
      <div className="mt-6 flex flex-col gap-3">
        <div className="flex items-center justify-between gap-3 rounded-card bg-raised px-4 py-3">
          <code className="text-body font-medium tracking-wider break-all">{secret}</code>
          <button type="button" onClick={() => navigator.clipboard?.writeText(secret)} aria-label="Copy the key" className="hit-slop grid size-8 flex-none place-items-center rounded-full text-muted hover:bg-surface hover:text-text"><Copy className="size-4" /></button>
        </div>
        <a href={form.totpUri} className="text-small text-muted underline">On your phone? Open it in your authenticator</a>
        <label htmlFor="otp" className="mt-3 text-body font-medium">Verification code</label>
        <CodeInput value={code} onChange={setCode} onComplete={onCode} />
        <p id="otp-help" className="text-small text-muted">{busy ? "Signing in…" : "Keep the key safe. If you lose your phone, the person who runs the server can reset it."}</p>
      </div>
    </>
  );
}

/** The shared secret inside an otpauth:// address. */
const secretOf = (uri: string) => (URL.canParse(uri) ? (new URL(uri).searchParams.get("secret") ?? "") : "");

function SourceStep({ form, set }: StepProps) {
  return (
    <>
      <StepTitle title="Where are your documents?">Atlas reads them where they are and never moves or changes them.</StepTitle>
      <Segmented className="mt-6" label="Source kind" value={form.kind} onChange={(kind) => set({ kind, check: "idle" })}
        options={[{ value: "folder", label: "Network folder" }, { value: "upload", label: "Upload files" }]} />
      {form.kind === "folder" ? <FolderPicker form={form} set={set} /> : (
        <div className="mt-5 flex min-h-32 flex-col items-center justify-center gap-2 rounded-card border border-dashed border-control p-6 text-center">
          <Upload aria-hidden className="size-5 text-muted" />
          <p className="text-body">Drop PDFs, Word or Excel files here after setup, from the Assistant or Sources.</p>
        </div>
      )}
    </>
  );
}

function FolderPicker({ form: { folder, check, checkDetail }, set }: StepProps) {
  const folderOk = FOLDER_PATH.test(folder.trim());
  const runCheck = async () => {
    set({ check: "checking" });
    try {
      const result = await api.checkFolder(folder.trim());
      set({ check: result.ok ? "ok" : "failed", checkDetail: result.detail });
    } catch {
      set({ check: "failed", checkDetail: "Atlas couldn't check this folder. Try again." });
    }
  };
  return (
    <div className="mt-5 flex flex-col gap-3">
      <Field label="Folder path" help="A folder Atlas may read, for example /sources/docs." error={folder && !folderOk ? "Use \\\\server\\share or an absolute path." : undefined}>
        {({ id, describedBy, invalid }) => (
          <div className="flex gap-2">
            <Input id={id} value={folder} onChange={(e) => set({ folder: e.target.value, check: "idle" })} placeholder="/sources/docs" aria-describedby={describedBy} aria-invalid={invalid} />
            <Button variant="outline" size="lg" disabled={!folderOk || check === "checking"} onClick={runCheck}>
              {check === "checking" ? <Spinner /> : <FolderOpen />}Check
            </Button>
          </div>
        )}
      </Field>
      {check === "ok" && <p role="status" className="flex items-center gap-2 text-body text-positive animate-enter"><Check className="size-4" />Atlas can read this folder with a read-only account.</p>}
      {check === "failed" && <p role="alert" className="text-body text-negative animate-enter">{checkDetail}</p>}
    </div>
  );
}
