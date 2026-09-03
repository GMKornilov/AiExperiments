"use client";

import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";

export const algorithmMethods = ["direct", "step-by-step", "generated-prompt", "experts"] as const;
export type AlgorithmMethod = (typeof algorithmMethods)[number];
export type AlgorithmLanguage = "python" | "java" | "cpp";
export type AlgorithmStatus = "idle" | "loading" | "success" | "error";

export type AlgorithmMessage = { role: "system" | "user" | "assistant"; content: string };
export type AlgorithmTrace = { step: "generate-prompt" | "solution"; messages: AlgorithmMessage[]; response?: string };
export type AlgorithmResult = {
  method: AlgorithmMethod;
  status: AlgorithmStatus;
  answer: string;
  trace: AlgorithmTrace[];
  error?: string;
};

type AlgorithmContextValue = {
  statement: string;
  language: AlgorithmLanguage;
  results: Record<AlgorithmMethod, AlgorithmResult>;
  activeMethod: AlgorithmMethod;
  isPending: boolean;
  setStatement: (value: string) => void;
  setLanguage: (value: AlgorithmLanguage) => void;
  setActiveMethod: (method: AlgorithmMethod) => void;
  run: () => void;
};

const methodError = "Не удалось получить ответ. Проверьте соединение и повторите запуск.";
const labels: Record<AlgorithmMethod, string> = {
  direct: "Прямой ответ",
  "step-by-step": "Решай пошагово",
  "generated-prompt": "Сначала промпт, затем решение",
  experts: "Группа экспертов",
};

function idleResult(method: AlgorithmMethod): AlgorithmResult {
  return { method, status: "idle", answer: "", trace: [] };
}

function initialResults(): Record<AlgorithmMethod, AlgorithmResult> {
  return Object.fromEntries(algorithmMethods.map((method) => [method, idleResult(method)])) as Record<AlgorithmMethod, AlgorithmResult>;
}

function isTrace(value: unknown): value is AlgorithmTrace[] {
  return Array.isArray(value) && value.every((step) => {
    if (!step || typeof step !== "object" || Array.isArray(step)) return false;
    const candidate = step as Record<string, unknown>;
    return (candidate.step === "generate-prompt" || candidate.step === "solution")
      && Array.isArray(candidate.messages)
      && candidate.messages.every((message) => message && typeof message === "object" && !Array.isArray(message)
        && ["system", "user", "assistant"].includes(String((message as Record<string, unknown>).role))
        && typeof (message as Record<string, unknown>).content === "string")
      && (candidate.response === undefined || typeof candidate.response === "string");
  });
}

function readEnvelope(value: unknown, method: AlgorithmMethod): AlgorithmResult | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const candidate = value as Record<string, unknown>;
  if (candidate.method !== method || typeof candidate.answer !== "string" || !isTrace(candidate.trace)) return null;
  if (candidate.status === "success") return { method, status: "success", answer: candidate.answer, trace: candidate.trace };
  if (candidate.status === "error" && candidate.error && typeof candidate.error === "object") {
    const message = (candidate.error as Record<string, unknown>).message;
    return { method, status: "error", answer: candidate.answer, trace: candidate.trace, error: typeof message === "string" ? message : methodError };
  }
  return null;
}

async function requestMethod(method: AlgorithmMethod, snapshot: { statement: string; language: AlgorithmLanguage }) {
  const response = await fetch(`/api/algorithms/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(snapshot),
  });
  const payload: unknown = await response.json().catch(() => null);
  return readEnvelope(payload, method);
}

const AlgorithmsContext = createContext<AlgorithmContextValue | null>(null);

export function AlgorithmsProvider({ children }: { children: ReactNode }) {
  const [statement, setStatement] = useState("");
  const [language, setLanguage] = useState<AlgorithmLanguage>("python");
  const [results, setResults] = useState<Record<AlgorithmMethod, AlgorithmResult>>(initialResults);
  const [activeMethod, setActiveMethod] = useState<AlgorithmMethod>("direct");
  const generation = useRef(0);
  const isPending = algorithmMethods.some((method) => results[method].status === "loading");

  const run = useCallback(() => {
    if (isPending) return;
    const snapshot = { statement, language };
    const currentGeneration = generation.current + 1;
    generation.current = currentGeneration;
    setActiveMethod("direct");
    setResults(Object.fromEntries(algorithmMethods.map((method) => [method, { ...idleResult(method), status: "loading" as const }])) as Record<AlgorithmMethod, AlgorithmResult>);

    for (const method of algorithmMethods) {
      void requestMethod(method, snapshot)
        .then((result) => result ?? { method, status: "error" as const, answer: "", trace: [], error: methodError })
        .catch(() => ({ method, status: "error" as const, answer: "", trace: [], error: methodError }))
        .then((result) => {
          if (generation.current !== currentGeneration) return;
          setResults((current) => ({ ...current, [method]: result }));
        });
    }
  }, [isPending, language, statement]);

  const value = useMemo(() => ({
    statement, language, results, activeMethod, isPending, setStatement, setLanguage, setActiveMethod, run,
  }), [activeMethod, isPending, language, results, run, statement]);

  return <AlgorithmsContext.Provider value={value}>{children}</AlgorithmsContext.Provider>;
}

export function useAlgorithms(): AlgorithmContextValue {
  const context = useContext(AlgorithmsContext);
  if (!context) throw new Error("useAlgorithms must be used inside AlgorithmsProvider");
  return context;
}

export { labels as algorithmMethodLabels };
