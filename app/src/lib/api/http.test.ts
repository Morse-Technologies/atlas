import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, httpApi, readEvents } from "./http";

type Reply = { status?: number; body?: unknown; headers?: Record<string, string> };

/** Replaces fetch with a queue of canned replies and records what was asked. */
function server(...replies: Reply[]) {
  const calls: { url: string; init: RequestInit }[] = [];
  const queue = [...replies];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    const r = queue.shift() ?? { status: 500 };
    const status = r.status ?? 200;
    const body = status === 204 || r.body === undefined ? null : JSON.stringify(r.body);
    return new Response(body, { status, headers: { "Content-Type": "application/json", ...r.headers } });
  }));
  return calls;
}

const replace = vi.fn();
beforeEach(() => {
  replace.mockReset();
  vi.stubGlobal("window", { location: { pathname: "/documents", search: "?q=x", replace } });
});
afterEach(() => vi.unstubAllGlobals());

describe("lists", () => {
  it("reads every page, following Next-Cursor, and asks for the largest page", async () => {
    const calls = server(
      { body: [{ id: "a" }], headers: { "Next-Cursor": "c1" } },
      { body: [{ id: "b" }] },
    );
    const sources = await httpApi.people();
    expect(sources).toEqual([{ id: "a" }, { id: "b" }]);
    expect(calls[0].url).toBe("/api/v1/users?limit=200");
    expect(calls[1].url).toBe("/api/v1/users?limit=200&cursor=c1");
  });

  it("builds the document query from the filter, leaving out 'all'", async () => {
    const calls = server({ body: [] });
    await httpApi.documents({ q: "torque", band: "all", status: "current", sourceId: "s1" });
    const query = new URL(calls[0].url, "http://x").searchParams;
    expect(query.get("q")).toBe("torque");
    expect(query.get("status")).toBe("current");
    expect(query.get("source_id")).toBe("s1");
    expect(query.has("band")).toBe(false);
  });

  it("gives every document the address of its current PDF", async () => {
    server({ body: [{ id: "d1", current_version_id: "v9", versions: [] }] });
    const [doc] = await httpApi.documents();
    expect(doc.sample).toBe("/api/v1/documents/d1/versions/v9/file");
  });
});

describe("writes", () => {
  it("send the idempotency key and a snake_case body", async () => {
    const calls = server({ body: { id: "a1" } });
    await httpApi.updateAlert("a1", { status: "dismissed", assigneeId: "u1", note: "ok" }, "key-1");
    const headers = calls[0].init.headers as Record<string, string>;
    expect(headers["Idempotency-Key"]).toBe("key-1");
    expect(calls[0].init.method).toBe("PATCH");
    expect(JSON.parse(calls[0].init.body as string)).toEqual({ status: "dismissed", assignee_id: "u1", note: "ok" });
  });

  it("answer 204 with no body", async () => {
    server({ status: 204 });
    await expect(httpApi.deleteUser("u1", "k")).resolves.toBeUndefined();
  });

  it("upload files as multipart, with no JSON content type", async () => {
    const calls = server({ body: { accepted: 1, rejected: [] } });
    const result = await httpApi.uploadFiles([new File(["x"], "a.pdf")]);
    expect(result.accepted).toBe(1);
    expect(calls[0].init.body).toBeInstanceOf(FormData);
    expect((calls[0].init.headers as Record<string, string>)["Content-Type"]).toBeUndefined();
  });
});

describe("errors", () => {
  it("a 401 outside sign-in sends the browser to /login and remembers where it was", async () => {
    server({ status: 401, body: { title: "Unauthorized" } });
    await expect(httpApi.me()).rejects.toBeInstanceOf(ApiError);
    expect(replace).toHaveBeenCalledWith("/login?next=%2Fdocuments%3Fq%3Dx");
  });

  it("a 401 while signing in is a wrong password, not a lost session", async () => {
    server({ status: 401, body: { title: "Unauthorized" } });
    await expect(httpApi.login("a@b.test", "nope")).rejects.toMatchObject({ status: 401 });
    expect(replace).not.toHaveBeenCalled();
  });

  it("carry the status and the server's own words", async () => {
    server({ status: 422, body: { title: "Unprocessable Entity", detail: "dismissing an alert needs a reason" } });
    await expect(httpApi.updateAlert("a", {}, "k")).rejects.toMatchObject({ status: 422, message: "dismissing an alert needs a reason" });
  });

  it("survive an error body that is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" })));
    await expect(httpApi.me()).rejects.toMatchObject({ status: 502, message: "Bad Gateway" });
  });
});

describe("overview", () => {
  it("fills the bands the server left out with zero", async () => {
    server({ body: { today: {}, week: [], top_topics: [], documents: { indexed: 3, by_band: { procedure: 3 }, processing: 0, quarantined: 0 }, open_alerts: {}, finding: "" } });
    const o = await httpApi.overview();
    expect(o.documents.byBand).toEqual({ procedure: 3, manual: 0, norm: 0, erp: 0 });
  });
});

/** A stream that delivers the given strings as separate chunks. */
const stream = (...chunks: string[]) =>
  new ReadableStream<Uint8Array>({
    start(c) {
      chunks.forEach((s) => c.enqueue(new TextEncoder().encode(s)));
      c.close();
    },
  });

async function drain<T>(events: AsyncIterable<T>) {
  const out: T[] = [];
  for await (const e of events) out.push(e);
  return out;
}

const collect = (body: ReadableStream<Uint8Array>) => drain(readEvents(body));

describe("readEvents", () => {
  it("turns frames into events, even when a frame is split across chunks", async () => {
    const events = await collect(stream(
      'data: {"type":"meta","answer_id":"a1","citations":[],"notes":[]}\n\ndata: {"type":"tok',
      'en","t":"Hello "}\n\n: ping\n\ndata: {"type":"done","answer":{"id":"a1","conversation_id":"c1"}}\n\n',
    ));
    expect(events).toEqual([
      { type: "meta", answerId: "a1", citations: [], notes: [] },
      { type: "token", t: "Hello " },
      { type: "done", answer: { id: "a1", conversationId: "c1" } },
    ]);
  });

  it("reports a stream that ends before done", async () => {
    const events = await collect(stream('data: {"type":"token","t":"Hi"}\n\n'));
    expect(events.at(-1)).toMatchObject({ type: "error" });
  });

  it("does not add an error after an error event the server sent", async () => {
    const events = await collect(stream('data: {"type":"error","message":"boom"}\n\n'));
    expect(events).toEqual([{ type: "error", message: "boom" }]);
  });
});

describe("ask", () => {
  it("fails like any request when the question cannot start", async () => {
    server({ status: 404, body: { title: "Not Found" } });
    await expect(drain(httpApi.ask("q", { conversationId: "gone" }))).rejects.toMatchObject({ status: 404 });
  });

  it("posts the question as snake_case JSON asking for an event stream", async () => {
    const calls = server({ status: 404 });
    await expect(drain(httpApi.ask("q", { conversationId: "c1", scope: null }))).rejects.toBeInstanceOf(ApiError);
    const headers = calls[0].init.headers as Record<string, string>;
    expect(headers.Accept).toBe("text/event-stream");
    expect(JSON.parse(calls[0].init.body as string)).toEqual({ question: "q", conversation_id: "c1", scope: null });
  });
});
