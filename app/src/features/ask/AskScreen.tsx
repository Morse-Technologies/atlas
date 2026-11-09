"use client";
import Link from "next/link";
import { useMod } from "@/lib/platform";
import { useRouter, useSearchParams } from "next/navigation";
import { History, SquarePen } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { newKey, useAskStream, useConversation, useConversations, useDocuments, useFeedback, usePeople } from "@/features/hooks";
import type { Answer, Document, Person } from "@/lib/api/types";
import { cn } from "@/lib/cn";
import { Button, Kbd, Mark, Skeleton, Tooltip, buttonStyles } from "@/ui/atoms";
import { ErrorState, Sheet } from "@/ui/cells";
import { AskComposer, AssistantMessage, HistoryList, UserMessage } from "@/ui/organisms/Assistant";

const ALL_SCOPES = "all";
const SCOPES = [ALL_SCOPES, "Line 1", "Line 2", "Line 3"];
const HISTORY_SKELETON_ROWS = 5;
const SUGGESTION_DELAY_MS = 120;
const SUGGESTION_STAGGER_MS = 50;
const SUGGESTIONS = [
  "What's the tightening torque for the spindle on CNC-07?",
  "What do I need to lock out before opening the CNC-08 guard?",
  "What coolant concentration should CNC-07 run at?",
];

function useLookups() {
  const { data: docs } = useDocuments({});
  const { data: people } = usePeople();
  return {
    docs: useMemo(() => new Map<string, Document>((docs ?? []).map((d) => [d.id, d])), [docs]),
    people: useMemo(() => new Map<string, Person>((people ?? []).map((p) => [p.id, p])), [people]),
  };
}

type Lookups = ReturnType<typeof useLookups>;

/** One chat: the answers asked in this visit, the live stream, and the question from `?q=` asked once. */
function useAskSession(conversationId: string | undefined, scope: string) {
  const initialQ = useSearchParams().get("q") ?? "";
  const { state, ask, reset } = useAskStream();
  const [localAnswers, setLocalAnswers] = useState<Answer[]>([]);
  const [convId, setConvId] = useState(conversationId);
  const asked = useRef("");

  const submit = async (question: string) => {
    const answer = await ask(question, { conversationId: convId, scope: scope === ALL_SCOPES ? null : scope });
    if (!answer) return;
    setLocalAnswers((a) => [...a, answer]);
    reset();
    if (convId) return;
    setConvId(answer.conversationId);
    window.history.replaceState(null, "", `/ask/${answer.conversationId}`);
  };

  useEffect(() => {
    if (initialQ && asked.current !== initialQ && !conversationId) { asked.current = initialQ; submit(initialQ); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQ]);

  return { state, reset, submit, localAnswers, convId };
}

/** ⌘⇧O starts a new chat. */
function useNewChatShortcut() {
  const router = useRouter();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === "o") { e.preventDefault(); router.push("/ask"); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [router]);
}

/** Optimistic thumbs up/down that rolls back if the server refuses it. */
function useVotes() {
  const feedback = useFeedback();
  const [votes, setVotes] = useState<Record<string, "up" | "down">>({});
  const vote = (answerId: string, value: "up" | "down") => {
    const before = votes[answerId];
    setVotes((m) => ({ ...m, [answerId]: value }));
    feedback.mutate({ id: answerId, value, key: newKey() }, { onError: () => setVotes((m) => ({ ...m, [answerId]: before })) });
  };
  return { votes, vote };
}

/** The Assistant: history on the left, the chat in the middle, a floating composer at the foot. */
export function AskScreen({ conversationId }: { conversationId?: string }) {
  const lookups = useLookups();
  const conv = useConversation(conversationId ?? "");
  const [draft, setDraft] = useState("");
  const [scope, setScope] = useState(ALL_SCOPES);
  const [historyOpen, setHistoryOpen] = useState(false);
  const session = useAskSession(conversationId, scope);
  useNewChatShortcut();

  const turns: Answer[] = [...(conv.data?.answers ?? []), ...session.localAnswers];
  const streaming = session.state.status === "streaming" ? session.state : null;
  const empty = !conversationId && turns.length === 0 && !streaming && session.state.status !== "error";
  const title = conv.data?.conversation.title ?? turns[0]?.question ?? (streaming ? streaming.question : "New chat");
  const send = (q: string) => { setDraft(""); return session.submit(q); };
  const composer = (large?: boolean) => (
    <AskComposer value={draft} onChange={setDraft} onSubmit={() => send(draft)} onStop={session.reset} busy={!!streaming}
      scope={scope} onScopeChange={setScope} scopes={SCOPES} autoFocus={large} large={large}
      placeholder={large ? "Ask about a procedure, a policy or a norm" : "Ask a follow-up, for example “and for the other site?”"} />
  );

  return (
    <div className="flex min-h-0 flex-1 md:h-[calc((100dvh-66px)/var(--content-zoom))]">
      <HistoryAside activeId={session.convId} />
      <section aria-label="Chat" className="flex min-w-0 flex-1 flex-col">
        <ChatHeader title={empty ? null : title} onOpenHistory={() => setHistoryOpen(true)} />
        {empty ? <EmptyChat composer={composer(true)} onPick={send} /> : (
          <>
            <ChatTurns conv={conv} turns={turns} session={session} lookups={lookups} onAgain={send} showLoading={!!conversationId} />
            <div className={cn("sticky bottom-14 mx-auto w-full max-w-[760px] bg-surface px-4 pt-2 pb-4 md:static md:pb-5")}>
              {composer()}
              <p className="mt-2 text-center text-small text-muted">Answers are written by AI from indexed documents only. Check the cited page before acting on them.</p>
            </div>
          </>
        )}
      </section>
      <Sheet open={historyOpen} onOpenChange={setHistoryOpen} title="Chats" side="left" width="w-[min(320px,85vw)]">
        <div className="p-3">
          <Link href="/ask" onClick={() => setHistoryOpen(false)} className="flex h-10 items-center gap-2 rounded-full border border-control px-3.5 text-body">
            <SquarePen className="size-4" />New chat
          </Link>
        </div>
        <div className="px-1.5 pb-6"><ChatHistory activeId={session.convId} onPick={() => setHistoryOpen(false)} /></div>
      </Sheet>
    </div>
  );
}

function ChatHistory({ activeId, onPick }: { activeId?: string; onPick?: () => void }) {
  const conversations = useConversations();
  if (conversations.isPending) {
    return <div className="flex flex-col gap-2 px-2.5">{Array.from({ length: HISTORY_SKELETON_ROWS }, (_, i) => <Skeleton key={i} className="h-5 w-full" />)}</div>;
  }
  return <HistoryList items={conversations.data ?? []} activeId={activeId} onPick={onPick} />;
}

function HistoryAside({ activeId }: { activeId?: string }) {
  const mod = useMod();
  return (
    <aside aria-label="Chat history" className="hidden w-64 flex-none flex-col border-r border-border bg-ground md:flex">
      <div className="px-5 pt-5 pb-3">
        <Link href="/ask" className="flex h-9 items-center justify-between gap-2 rounded-full border border-control px-3.5 text-body transition-colors duration-[var(--dur-1)] hover:bg-active/50">
          <span className="inline-flex items-center gap-2"><SquarePen className="size-4" />New chat</span>
          <span className="flex gap-0.5"><Kbd>{mod}</Kbd><Kbd>⇧</Kbd><Kbd>O</Kbd></span>
        </Link>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-4"><ChatHistory activeId={activeId} /></div>
    </aside>
  );
}

function ChatHeader({ title, onOpenHistory }: { title: string | null; onOpenHistory: () => void }) {
  const mod = useMod();
  return (
    <header className="flex h-12 flex-none items-center gap-2 px-3 md:px-5">
      <Tooltip label="History">
        <button type="button" onClick={onOpenHistory} aria-label="Open chat history"
          className="hit-slop grid size-8 place-items-center rounded-full text-muted hover:bg-raised hover:text-text md:hidden">
          <History className="size-4" />
        </button>
      </Tooltip>
      {title && <h1 className="min-w-0 flex-1 truncate text-body font-medium">{title}</h1>}
      <Tooltip label={`New chat (${mod} Shift O)`}>
        <Link href="/ask" aria-label="New chat" className="hit-slop ml-auto grid size-8 place-items-center rounded-full text-muted hover:bg-raised hover:text-text md:hidden">
          <SquarePen className="size-4" />
        </Link>
      </Tooltip>
    </header>
  );
}

function EmptyChat({ composer, onPick }: { composer: ReactNode; onPick: (q: string) => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-4 pb-16">
      <div className="w-full max-w-[720px] animate-enter">
        <div className="flex flex-col items-center text-center">
          <span className="grid size-14 place-items-center rounded-full bg-raised"><Mark className="size-8" /></span>
          <h1 className="mt-5 text-title font-medium tracking-[-0.01em]">Ask Atlas</h1>
          <p className="mt-1.5 text-body text-muted">Answers come only from your organization&apos;s documents, and every claim points to its page.</p>
        </div>
        <div className="mt-8">{composer}</div>
        <ul className="mt-4 flex flex-wrap justify-center gap-2" aria-label="Try asking">
          {SUGGESTIONS.map((s, i) => (
            <li key={s} className="animate-enter" style={{ animationDelay: `${SUGGESTION_DELAY_MS + i * SUGGESTION_STAGGER_MS}ms` }}>
              <button type="button" onClick={() => onPick(s)}
                className="rounded-full border border-border px-3.5 py-1.5 text-small text-muted transition-[border-color,color,transform] duration-[var(--dur-1)] ease-out hover:border-control hover:text-text active:scale-[0.97]">
                {s}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function MissingConversation({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="mx-auto mt-16 max-w-md text-center">
      <h2 className="text-section font-medium">This conversation doesn&apos;t exist</h2>
      <p className="mt-1 text-body text-muted">It may have been deleted, or the link is wrong.</p>
      <div className="mt-4 flex justify-center gap-2">
        <Link href="/ask" className={buttonStyles({ variant: "primary" })}><SquarePen />New chat</Link>
        <Button variant="ghost" onClick={onRetry}>Try again</Button>
      </div>
    </div>
  );
}

function ChatTurns({ conv, turns, session, lookups, onAgain, showLoading }: {
  conv: ReturnType<typeof useConversation>; turns: Answer[]; session: ReturnType<typeof useAskSession>;
  lookups: Lookups; onAgain: (q: string) => void; showLoading: boolean;
}) {
  const { state } = session;
  const streaming = state.status === "streaming" ? state : null;
  const [active, setActive] = useState<number | null>(null);
  const { votes, vote } = useVotes();
  const scroller = useRef<HTMLDivElement>(null);

  // Keep the newest message in view while it streams.
  useEffect(() => {
    const el = scroller.current;
    if (el && streaming) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  }, [streaming]);

  return (
    <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-[760px] flex-col gap-8 px-4 pt-6 pb-10">
        {showLoading && conv.isPending && (
          <div className="flex flex-col gap-6" aria-busy="true" aria-label="Loading conversation">
            <Skeleton className="ml-auto h-11 w-[60%] rounded-[22px]" />
            <Skeleton className="h-4 w-[90%]" /><Skeleton className="h-4 w-[70%]" />
          </div>
        )}
        {conv.isError && <MissingConversation onRetry={() => conv.refetch()} />}
        {turns.map((a, i) => {
          const last = i === turns.length - 1 && !streaming;
          return (
            <div key={a.id} className="flex flex-col gap-5">
              <UserMessage text={a.question} />
              <AssistantMessage
                answer={votes[a.id] ? { ...a, feedback: votes[a.id] } : a}
                citations={a.citations} notes={a.notes} {...lookups}
                active={last ? active : null} onHover={setActive} conversationId={a.conversationId}
                onFeedback={(v) => vote(a.id, v)}
                onRetry={last ? () => onAgain(a.question) : undefined}
              />
            </div>
          );
        })}
        {streaming && (
          <div className="flex flex-col gap-5">
            <UserMessage text={streaming.question} />
            <AssistantMessage streamText={streaming.text} citations={streaming.citations} notes={streaming.notes}
              {...lookups} active={active} onHover={setActive} />
          </div>
        )}
        {state.status === "error" && <ErrorState what="an answer from the local model" onRetry={() => onAgain(state.question)} />}
      </div>
    </div>
  );
}
