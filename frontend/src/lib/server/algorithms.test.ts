import { afterEach, describe, expect, it, vi } from "vitest";
import { algorithmMethods, algorithmTransportTimeoutMilliseconds, createAlgorithmsDispatcher, proxyAlgorithmRequest } from "./algorithms";

const rawStatement = "  Найдите пару чисел.\nОграничение: n ≤ 10\nПример: 2,7 → 9  ";

function request(body: unknown, contentType = "application/json") {
  return new Request("http://frontend/api/algorithms/direct", {
    method: "POST",
    headers: { "Content-Type": contentType },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
}

function success(method: string, trace: unknown[] = []) {
  return { method, status: "success", answer: "Неполный, но видимый Markdown", trace };
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("proxyAlgorithmRequest", () => {
  it("creates a dedicated transport with timeouts above the longest BFF deadline", () => {
    let options: unknown;
    class FakeAgent {
      constructor(value: unknown) { options = value; }
    }

    createAlgorithmsDispatcher(FakeAgent as unknown as typeof import("undici").Agent);

    expect(options).toEqual({ headersTimeout: algorithmTransportTimeoutMilliseconds, bodyTimeout: algorithmTransportTimeoutMilliseconds });
    expect(algorithmTransportTimeoutMilliseconds).toBeGreaterThan(365_000);
  });

  it("forwards the exact snapshot once to every documented private endpoint", async () => {
    vi.stubEnv("ALGORITHMS_BACKEND_URL", "http://private-go:8080");
    const fetchMock = vi.fn((url: URL, init?: RequestInit) => {
      void init;
      return Response.json(success(url.pathname.split("/").at(-1)!));
    });
    vi.stubGlobal("fetch", fetchMock);

    for (const method of algorithmMethods) {
      const response = await proxyAlgorithmRequest(request({ statement: rawStatement, language: "cpp" }), method);
      expect(response.status).toBe(200);
      expect(await response.json()).toEqual(success(method));
    }

    expect(fetchMock).toHaveBeenCalledTimes(4);
    for (const [index, method] of algorithmMethods.entries()) {
      expect(fetchMock.mock.calls[index][0]).toEqual(new URL(`http://private-go:8080/api/algorithms/${method}`));
      expect(fetchMock.mock.calls[index][1]).toEqual(expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ statement: rawStatement, language: "cpp" }),
        cache: "no-store",
      }));
    }
  });

  it.each([
    ["not JSON", "not json"],
    ["unknown language", { statement: rawStatement, language: "go" }],
    ["array language", { statement: rawStatement, language: ["python"] }],
    ["nested array language", { statement: rawStatement, language: [["python"]] }],
    ["empty statement", { statement: " \n ", language: "python" }],
    ["extra field", { statement: rawStatement, language: "java", examples: [] }],
    ["too many Unicode code points", { statement: "😀".repeat(10_001), language: "python" }],
  ])("rejects %s before contacting Go", async (_name, body) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await proxyAlgorithmRequest(request(body), "direct");

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toMatchObject({ method: "direct", status: "error", answer: "", error: { code: "invalid_request" } });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects a non-JSON content type before contacting Go", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await proxyAlgorithmRequest(request("plain text", "text/plain"), "experts");

    expect(response.status).toBe(415);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps a valid partial meta trace while hiding private upstream details", async () => {
    const secret = "Bearer private-key and http://private-go:8080/internal";
    const trace = [{
      step: "generate-prompt",
      messages: [{ role: "system", content: "create prompt" }, { role: "user", content: rawStatement }],
      response: "  raw generated prompt\n",
    }, {
      step: "solution",
      messages: [{ role: "system", content: "solve" }, { role: "user", content: "raw generated prompt" }],
    }];
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({
      method: "generated-prompt", status: "error", answer: "", trace,
      error: { code: "unavailable", message: secret },
    }, { status: 502 })));

    const response = await proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "generated-prompt");
    const body = await response.json();

    expect(response.status).toBe(502);
    expect(body).toMatchObject({ status: "error", answer: "", trace, error: { code: "unavailable" } });
    expect(JSON.stringify(body)).not.toContain(secret);
  });

  it("rejects malformed backend envelopes instead of passing them to the browser", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({
      method: "direct", status: "success", answer: "looks fine", trace: [{ step: "solution", messages: [{ role: "tool", content: "bad" }] }],
    })));

    const response = await proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "direct");

    expect(response.status).toBe(502);
    await expect(response.json()).resolves.toMatchObject({ status: "error", answer: "", trace: [], error: { code: "invalid_response" } });
  });

  it("cancels and releases a hanging input stream after the local deadline", async () => {
    vi.useFakeTimers();
    let cancelled = false;
    const stream = new ReadableStream<Uint8Array>({ cancel: () => { cancelled = true; } });
    const responsePromise = proxyAlgorithmRequest(new Request("http://frontend/api/algorithms/direct", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: stream,
      duplex: "half",
    } as RequestInit & { duplex: "half" }), "direct");

    await vi.advanceTimersByTimeAsync(10_000);
    const response = await responsePromise;

    expect(response.status).toBe(400);
    expect(cancelled).toBe(true);
    expect(stream.locked).toBe(false);
    vi.useRealTimers();
  });

  it("rejects a complete-but-unclosed valid upload after the deadline without fetching", async () => {
    vi.useFakeTimers();
    let cancelled = false;
    const encoded = new TextEncoder().encode(JSON.stringify({ statement: "valid unfinished upload", language: "python" }));
    const stream = new ReadableStream<Uint8Array>({
      start(controller) { controller.enqueue(encoded); },
      cancel: () => { cancelled = true; },
    });
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const responsePromise = proxyAlgorithmRequest(new Request("http://frontend/api/algorithms/direct", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: stream, duplex: "half",
    } as RequestInit & { duplex: "half" }), "direct");

    await vi.advanceTimersByTimeAsync(10_000);
    const response = await responsePromise;

    expect(response.status).toBe(400);
    expect(cancelled).toBe(true);
    expect(stream.locked).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("returns a safe envelope when reading the input stream fails", async () => {
    const secret = "body-read-private-detail";
    const stream = new ReadableStream<Uint8Array>({ start(controller) { controller.error(new Error(secret)); } });

    const response = await proxyAlgorithmRequest(new Request("http://frontend/api/algorithms/direct", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: stream,
      duplex: "half",
    } as RequestInit & { duplex: "half" }), "direct");

    expect(response.status).toBe(400);
    expect(await response.text()).not.toContain(secret);
  });

  it.each(["success", "partial timeout"] as const)("keeps a legal near-limit meta trace on %s", async (scenario) => {
    const generatedPrompt = "x".repeat(1024 * 1024 - 1024);
    const statement = "s".repeat(10_000);
    const secondMessage = `Исходный statement:\n${statement}\n\nНеизменённый generated prompt:\n${generatedPrompt}`;
    const trace = [
      { step: "generate-prompt", messages: [{ role: "user", content: statement }], response: generatedPrompt },
      { step: "solution", messages: [{ role: "user", content: secondMessage }], response: scenario === "success" ? "answer" : undefined },
    ];
    const upstream = scenario === "success"
      ? { method: "generated-prompt", status: "success", answer: "answer", trace }
      : { method: "generated-prompt", status: "error", answer: "", trace, error: { code: "timeout", message: "private detail" } };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(upstream, { status: scenario === "success" ? 200 : 504 })));

    const response = await proxyAlgorithmRequest(request({ statement, language: "python" }), "generated-prompt");
    const body = await response.json();

    expect(new TextEncoder().encode(JSON.stringify(upstream)).byteLength).toBeLessThan(8 * 1024 * 1024);
    expect(response.status).toBe(scenario === "success" ? 200 : 504);
    expect(body).toMatchObject({ method: "generated-prompt", status: scenario === "success" ? "success" : "error", trace: JSON.parse(JSON.stringify(trace)) });
  });

  it("assigns documented BFF budgets to ordinary and meta requests", async () => {
    const budgets: number[] = [];
    vi.spyOn(AbortSignal, "timeout").mockImplementation((milliseconds) => {
      budgets.push(milliseconds);
      return new AbortController().signal;
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(success("direct"))));

    await proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "experts");
    await proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "generated-prompt");

    expect(budgets).toEqual([185_000, 365_000]);
  });

  it("returns timeout when the common deadline expires before backend headers", async () => {
    vi.useFakeTimers();
    vi.spyOn(AbortSignal, "timeout").mockImplementation((milliseconds) => {
      const controller = new AbortController();
      setTimeout(() => controller.abort(), milliseconds);
      return controller.signal;
    });
    vi.stubGlobal("fetch", vi.fn((_url: URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("expired", "TimeoutError")), { once: true });
    })));

    const responsePromise = proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "direct");
    await vi.advanceTimersByTimeAsync(185_000);
    const response = await responsePromise;

    expect(response.status).toBe(504);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "timeout" } });
    vi.useRealTimers();
  });

  it("returns timeout and releases the body reader when the common deadline expires after headers", async () => {
    vi.useFakeTimers();
    vi.spyOn(AbortSignal, "timeout").mockImplementation((milliseconds) => {
      const controller = new AbortController();
      setTimeout(() => controller.abort(), milliseconds);
      return controller.signal;
    });
    let cancelled = false;
    const stream = new ReadableStream<Uint8Array>({ cancel: () => { cancelled = true; } });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream, { headers: { "Content-Type": "application/json" } })));

    const responsePromise = proxyAlgorithmRequest(request({ statement: rawStatement, language: "python" }), "direct");
    await vi.advanceTimersByTimeAsync(185_000);
    const response = await responsePromise;

    expect(response.status).toBe(504);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "timeout" } });
    expect(cancelled).toBe(true);
    expect(stream.locked).toBe(false);
    vi.useRealTimers();
  });
});
