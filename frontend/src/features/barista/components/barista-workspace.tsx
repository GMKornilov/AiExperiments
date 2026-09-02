"use client";

import {
  FormEvent,
  KeyboardEvent,
  useRef,
  useState,
} from "react";
import { requestBarista } from "../lib/chat-client";
import {
  ChatMode,
  ChatResponse,
  ControlledAnswer,
  isControlledAnswer,
} from "../model/types";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import styles from "./barista-workspace.module.css";

type ViewState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "success"; response: ChatResponse; requestID: number };

const recipeMetrics: Array<{
  key: keyof ControlledAnswer["recipe"];
  label: string;
  unit: string;
}> = [
  { key: "coffee_g", label: "Кофе", unit: "г" },
  { key: "water_g", label: "Вода", unit: "г" },
  { key: "temperature_c", label: "Температура", unit: "°C" },
  { key: "brew_time_sec", label: "Время", unit: "с" },
];

export function BaristaWorkspace() {
  const [mode, setMode] = useState<ChatMode>("free");
  const [prompt, setPrompt] = useState("");
  const [view, setView] = useState<ViewState>({ status: "idle" });
  const requestSequence = useRef(0);
  const abortController = useRef<AbortController | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const modeInputs = useRef<Partial<Record<ChatMode, HTMLInputElement | null>>>({});

  const isLoading = view.status === "loading";

  async function submit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    const normalizedPrompt = prompt.trim();
    if (!normalizedPrompt || isLoading) return;

    abortController.current?.abort();
    const controller = new AbortController();
    abortController.current = controller;
    setView({ status: "loading" });

    try {
      const response = await requestBarista(mode, normalizedPrompt, controller.signal);
      requestSequence.current += 1;
      setView({
        status: "success",
        response,
        requestID: requestSequence.current,
      });
    } catch (error) {
      if (controller.signal.aborted) return;
      setView({
        status: "error",
        message:
          error instanceof Error ? error.message : "Не удалось получить ответ.",
      });
    } finally {
      if (abortController.current === controller) abortController.current = null;
    }
  }

  function handlePromptKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      formRef.current?.requestSubmit();
    }
  }

  function handleModeKeyDown(
    event: KeyboardEvent<HTMLInputElement>,
    value: ChatMode,
  ) {
    if (event.key === " " || event.key === "Enter") {
      event.preventDefault();
      setMode(value);
      return;
    }

    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) {
      return;
    }

    event.preventDefault();
    const nextMode: ChatMode = value === "free" ? "controlled" : "free";
    setMode(nextMode);
    modeInputs.current[nextMode]?.focus();
  }

  return (
    <div className={styles.workspace}>
      <section className={styles.composer} aria-labelledby="composer-title">
        <div className={styles.sectionHeading}>
          <h2 id="composer-title">Что приготовим?</h2>
          <p>Выберите, нужен ли структурированный рецепт.</p>
        </div>

        <form ref={formRef} onSubmit={submit}>
          <fieldset className={styles.modeSwitch} disabled={isLoading}>
            <legend>Режим ответа</legend>
            <div className={styles.modeOptions}>
              {(["free", "controlled"] as const).map((value) => (
                <label className={styles.modeOption} key={value}>
                  <input
                    ref={(input) => {
                      modeInputs.current[value] = input;
                    }}
                    type="radio"
                    name="mode"
                    value={value}
                    checked={mode === value}
                    onChange={() => setMode(value)}
                    onKeyDown={(event) => handleModeKeyDown(event, value)}
                  />
                  <span>{value === "free" ? "Свободный" : "Структурный"}</span>
                </label>
              ))}
            </div>
          </fieldset>

          <label className={styles.promptLabel} htmlFor="prompt">
            Ваш вопрос
          </label>
          <textarea
            id="prompt"
            name="prompt"
            rows={5}
            required
            maxLength={4000}
            value={prompt}
            disabled={isLoading}
            onChange={(event) => setPrompt(event.target.value)}
            onKeyDown={handlePromptKeyDown}
            placeholder="Например: эспрессо получается горьким — что изменить?"
            aria-describedby="prompt-hint"
          />

          <div className={styles.formFooter}>
            <p id="prompt-hint" className={styles.hint}>
              Enter — отправить · Shift + Enter — новая строка
            </p>
            <button type="submit" disabled={isLoading || !prompt.trim()}>
              <span>{isLoading ? "Настраиваем помол…" : "Спросить бариста"}</span>
              <span aria-hidden="true">→</span>
            </button>
          </div>
        </form>
      </section>

      <section
        className={styles.responsePanel}
        aria-live="polite"
        aria-busy={isLoading}
        aria-label="Ответ бариста"
      >
        {view.status === "idle" && <EmptyState />}
        {view.status === "loading" && <LoadingState />}
        {view.status === "error" && <ErrorState message={view.message} />}
        {view.status === "success" && (
          <ResultView key={view.requestID} response={view.response} />
        )}
      </section>
    </div>
  );
}

function EmptyState() {
  return (
    <div className={styles.emptyState}>
      <p className={styles.emptyMark} aria-hidden="true">01</p>
      <h2>Чашка ждёт вопроса</h2>
      <p>Ответ появится здесь — текстом или как точный рецепт.</p>
    </div>
  );
}

function LoadingState() {
  return (
    <div className={styles.loadingState} role="status">
      <span className={styles.loader} aria-hidden="true" />
      <p>Бариста настраивает помол…</p>
    </div>
  );
}

function ErrorState({ message }: { message: string }) {
  return (
    <div className={styles.errorState} role="alert">
      <p className={styles.eyebrow}>Не получилось</p>
      <h2>Ответ не заварился</h2>
      <p>{message}</p>
    </div>
  );
}

function ResultView({ response }: { response: ChatResponse }) {
  return (
    <div>
      {response.mode === "controlled" ? (
        isControlledAnswer(response.data) ? (
          <ControlledResult answer={response.data} />
        ) : (
          <GenericJSONResult data={response.data} />
        )
      ) : (
        <FreeResult text={response.raw} />
      )}

      <details className={styles.rawResponse}>
        <summary>
          Сырой ответ <span aria-hidden="true">⌄</span>
        </summary>
        <pre>{response.raw}</pre>
      </details>
    </div>
  );
}

function ResultHeading({ mode }: { mode: string }) {
  return (
    <div className={styles.answerHeading}>
      <h2>{mode === "Структурный режим" ? "Рецепт и настройка" : "Ответ бариста"}</h2>
      <p>{mode}</p>
    </div>
  );
}

function FreeResult({ text }: { text: string }) {
  return (
    <>
      <ResultHeading mode="Свободный режим" />
      <MarkdownContent className={styles.freeAnswer}>{text}</MarkdownContent>
    </>
  );
}

function ControlledResult({ answer }: { answer: ControlledAnswer }) {
  return (
    <>
      <ResultHeading mode="Структурный режим" />
      <p className={styles.sectionLabel}>Кратко</p>
      <MarkdownContent className={styles.summary}>{answer.summary}</MarkdownContent>
      <p className={styles.sectionLabel}>На что обратить внимание</p>
      <ol className={styles.focusPoints}>
        {answer.focus_points.map((point, index) => (
          <li key={`${index}-${point}`}><MarkdownContent>{point}</MarkdownContent></li>
        ))}
      </ol>
      <p className={styles.sectionLabel}>Параметры рецепта</p>
      <dl className={styles.metrics}>
        {recipeMetrics.map(({ key, label, unit }) => (
          <div className={styles.metric} key={key}>
            <dt>{label}</dt>
            <dd>{String(answer.recipe[key])} {unit}</dd>
          </div>
        ))}
      </dl>
    </>
  );
}

function GenericJSONResult({ data }: { data: unknown }) {
  return (
    <>
      <ResultHeading mode="JSON" />
      <div className={styles.jsonTree}>
        <JSONValue value={data} />
      </div>
    </>
  );
}

function JSONValue({ value, label }: { value: unknown; label?: string }) {
  const prefix = label ? <span className={styles.jsonKey}>{label}: </span> : null;
  if (value === null || typeof value !== "object") {
    return (
      <div className={styles.jsonNode}>
        {prefix}
        <span className={value === null ? styles.jsonNull : styles.jsonValue}>
          {value === null ? "null" : String(value)}
        </span>
      </div>
    );
  }

  const entries = Array.isArray(value)
    ? value.map((item, index) => [String(index), item] as const)
    : Object.entries(value);
  return (
    <div className={styles.jsonNode}>
      {prefix}
      {entries.map(([key, child]) => (
        <JSONValue key={key} label={key} value={child} />
      ))}
    </div>
  );
}
