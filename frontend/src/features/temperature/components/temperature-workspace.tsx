"use client";

import { FormEvent, useState } from "react";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import { requestTemperature } from "../lib/temperature-client";
import styles from "./temperature-workspace.module.css";

const quickTemperatures = [0, 0.7, 1.2] as const;

type ViewState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "success"; answer: string }
  | { status: "error"; message: string };

function parseTemperature(value: string): number | null {
  if (!value.trim()) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 && parsed <= 2 ? parsed : null;
}

export function TemperatureWorkspace() {
  const [prompt, setPrompt] = useState("");
  const [temperatureInput, setTemperatureInput] = useState("0.7");
  const [validationError, setValidationError] = useState("");
  const [view, setView] = useState<ViewState>({ status: "idle" });
  const isLoading = view.status === "loading";
  const parsedTemperature = parseTemperature(temperatureInput);

  async function run() {
    const normalizedPrompt = prompt.trim();
    if (!normalizedPrompt) {
      setValidationError("Введите промпт.");
      return;
    }
    if (Array.from(normalizedPrompt).length > 4_000) {
      setValidationError("Промпт должен содержать не более 4 000 символов.");
      return;
    }
    if (parsedTemperature === null) {
      setValidationError("Введите температуру — число от 0 до 2.");
      return;
    }

    const request = { prompt: normalizedPrompt, temperature: parsedTemperature };
    setValidationError("");
    setView({ status: "loading" });
    try {
      const response = await requestTemperature(request.prompt, request.temperature);
      setView({ status: "success", answer: response.answer.trim() });
    } catch (error) {
      setView({ status: "error", message: error instanceof Error ? error.message : "Не удалось получить ответ." });
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!isLoading) void run();
  }

  function selectQuickTemperature(value: number) {
    setTemperatureInput(String(value));
    setValidationError("");
  }

  return <div className={styles.workspace}>
    <section className={styles.composer} aria-labelledby="temperature-form-title">
      <div className={styles.sectionHeading}>
        <h2 id="temperature-form-title">Настройте вариативность</h2>
        <p>Один prompt, один ответ и точная температура генерации.</p>
      </div>
      <form noValidate onSubmit={submit}>
        <label className={styles.fieldLabel} htmlFor="temperature-prompt">Промпт</label>
        <textarea id="temperature-prompt" name="prompt" rows={7} value={prompt} onChange={(event) => setPrompt(event.target.value)} aria-describedby="temperature-prompt-hint temperature-error" aria-invalid={Boolean(validationError)} placeholder="Например: придумай короткий слоган для кофейни." />
        <p id="temperature-prompt-hint" className={styles.hint}>Enter добавляет новую строку.</p>

        <fieldset className={styles.temperatureControl} aria-describedby="temperature-error">
          <legend>Температура</legend>
          <div className={styles.quickChoices} aria-label="Быстрый выбор температуры">
            {quickTemperatures.map((value) => {
              const selected = parsedTemperature === value;
              return <button key={value} type="button" className={selected ? styles.quickChoiceActive : styles.quickChoice} aria-pressed={selected} onClick={() => selectQuickTemperature(value)}>{value}</button>;
            })}
          </div>
          <label className={styles.numberLabel} htmlFor="temperature-value">Своё значение</label>
          <input id="temperature-value" name="temperature" type="number" inputMode="decimal" min="0" max="2" step="any" value={temperatureInput} onChange={(event) => { setTemperatureInput(event.target.value); setValidationError(""); }} aria-invalid={Boolean(validationError && parsedTemperature === null)} />
        </fieldset>
        {validationError && <p id="temperature-error" className={styles.validationError} role="alert">{validationError}</p>}

        <div className={styles.formFooter}>
          <p className={styles.hint}>Допустимый диапазон: от 0 до 2.</p>
          <button type="submit" disabled={isLoading}><span>{isLoading ? "Генерируем…" : "Отправить"}</span><span aria-hidden="true">→</span></button>
        </div>
      </form>
    </section>

    <section className={styles.responsePanel} aria-live="polite" aria-busy={isLoading} aria-label="Ответ с выбранной температурой">
      {view.status === "idle" && <EmptyState />}
      {view.status === "loading" && <LoadingState />}
      {view.status === "success" && <ResultState answer={view.answer} />}
      {view.status === "error" && <ErrorState message={view.message} onRetry={() => void run()} />}
    </section>
  </div>;
}

function EmptyState() {
  return <div className={styles.emptyState}><p className={styles.emptyMark} aria-hidden="true">04</p><h2>Ответ ждёт настройки</h2><p>Введите prompt и выберите температуру.</p></div>;
}

function LoadingState() {
  return <div className={styles.loadingState} role="status"><span className={styles.loader} aria-hidden="true" /><p>Модель формулирует ответ…</p></div>;
}

function ResultState({ answer }: { answer: string }) {
  return <div><div className={styles.answerHeading}><h2>Ответ</h2><p>Готово</p></div><MarkdownContent className={styles.answer}>{answer}</MarkdownContent></div>;
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return <div className={styles.errorState} role="alert"><p className={styles.eyebrow}>Не получилось</p><h2>Ответ не получен</h2><p>{message}</p><button type="button" className={styles.retryButton} onClick={onRetry}>Повторить</button></div>;
}
