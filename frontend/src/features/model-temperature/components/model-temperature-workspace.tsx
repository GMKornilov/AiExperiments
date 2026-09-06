"use client";

import { FormEvent, useRef, useState } from "react";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import {
  type Model,
  type ModelTemperatureResponse,
  type Provider,
  providerModels,
  requestModelTemperature,
} from "../lib/model-temperature-client";
import styles from "@/features/temperature/components/temperature-workspace.module.css";
import comparisonStyles from "./model-comparison.module.css";

const providerLabels: Record<Provider, string> = { deepseek: "DeepSeek", kimi: "Kimi (Moonshot)" };
const comparisonModels = [
  { provider: "deepseek", model: "deepseek-v4-flash" },
  { provider: "deepseek", model: "deepseek-v4-pro" },
  { provider: "kimi", model: "kimi-k3" },
] as const;

type Comparison = { prompt: string; results: ViewState[] };

type ViewState =
  | { status: "loading" }
  | { status: "success"; response: ModelTemperatureResponse; provider: Provider; model: Model; temperature: number }
  | { status: "error"; message: string };

function modelLabel(provider: Provider, model: Model): string {
  return providerModels[provider].find((item) => item.id === model)?.label ?? model;
}

export function ModelTemperatureWorkspace() {
  const [prompt, setPrompt] = useState("");
  const [validationError, setValidationError] = useState("");
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const activeRequests = useRef(new Set<number>());
  const isLoading = Boolean(comparison?.results.some((result) => result.status === "loading"));

  async function compare(retryIndex?: number) {
    if (retryIndex === undefined) {
      if (activeRequests.current.size > 0) return;
    } else if (activeRequests.current.has(retryIndex) || comparison?.results[retryIndex]?.status !== "error") {
      return;
    }
    const snapshot = retryIndex === undefined ? prompt.trim() : comparison?.prompt ?? "";
    if (!snapshot || Array.from(snapshot).length > 4_000) {
      setValidationError(!snapshot ? "Введите промпт." : "Промпт должен содержать не более 4 000 символов.");
      return;
    }
    const indexes = retryIndex === undefined ? comparisonModels.map((_, index) => index) : [retryIndex];
    indexes.forEach((index) => activeRequests.current.add(index));
    setValidationError("");
    setComparison((previous) => ({
      prompt: snapshot,
      results: comparisonModels.map((_, index) =>
        retryIndex === undefined || index === retryIndex ? { status: "loading" } : previous!.results[index]),
    }));
    await Promise.all(indexes.map(async (index) => {
        const target = comparisonModels[index];
        let result: ViewState;
        try {
          const response = await requestModelTemperature(snapshot, 1, target.provider, target.model);
          result = { status: "success", response, ...target, temperature: 1 };
        } catch (error) {
          result = { status: "error", message: error instanceof Error ? error.message : "Не удалось получить ответ." };
        } finally {
          activeRequests.current.delete(index);
        }
        setComparison((previous) => previous && ({
          ...previous,
          results: previous.results.map((value, position) => position === index ? result : value),
        }));
    }));
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!isLoading) void compare();
  }


  return <div className={styles.workspace}>
    <section className={styles.composer} aria-labelledby="model-temperature-form-title">
      <div className={styles.sectionHeading}>
        <h2 id="model-temperature-form-title">Сравните ответы моделей</h2>
        <p>Один промпт — три независимых ответа с метриками.</p>
      </div>
      <form noValidate onSubmit={submit}>
        <label className={styles.fieldLabel} htmlFor="model-temperature-prompt">Промпт</label>
        <textarea id="model-temperature-prompt" name="prompt" rows={7} value={prompt} onChange={(event) => setPrompt(event.target.value)} aria-describedby="model-temperature-prompt-hint model-temperature-error" aria-invalid={Boolean(validationError)} placeholder="Например: придумай короткий слоган для кофейни." />
        <p id="model-temperature-prompt-hint" className={styles.hint}>Enter добавляет новую строку.</p>

        {validationError && <p id="model-temperature-error" className={styles.validationError} role="alert">{validationError}</p>}

        <div className={styles.formFooter}>
          <p className={styles.hint}>Сравнение: DeepSeek V4 Flash, DeepSeek V4 Pro и Kimi K3. Один промпт, температура 1 у всех. Три отдельных оплачиваемых запроса.</p>
          <button type="submit" disabled={isLoading}>{isLoading ? "Сравниваем…" : "Сравнить 3 модели"}</button>
        </div>
        <p className={styles.hint}>DeepSeek: reasoning выключен. Kimi K3: reasoning low (полностью отключить нельзя). Ожидание — до 3 минут.</p>
      </form>
    </section>

    {comparison ? <div className={comparisonStyles.grid}>
      {comparisonModels.map((target, index) => {
        const result = comparison.results[index];
        const label = modelLabel(target.provider, target.model);
        return <section key={target.model} className={`${styles.responsePanel} ${comparisonStyles.card}`} aria-label={label} aria-live="polite" aria-busy={result.status === "loading"}>
          <h2 className={comparisonStyles.title}>{label}</h2>
          <p className={styles.hint}>Температура: 1</p>
          {result.status === "loading" && <LoadingState />}
          {result.status === "success" && <ResultState {...result} />}
          {result.status === "error" && <ErrorState message={result.message} onRetry={() => void compare(index)} />}
        </section>;
      })}
    </div> : <section className={styles.responsePanel} aria-label="Ответы моделей">
      <EmptyState />
    </section>}
  </div>;
}

function EmptyState() {
  return <div className={styles.emptyState}><p className={styles.emptyMark} aria-hidden="true">05</p><h2>Ответы появятся здесь</h2><p>Введите промпт и запустите сравнение трёх моделей.</p></div>;
}

function LoadingState() {
  return <div className={styles.loadingState} role="status"><span className={styles.loader} aria-hidden="true" /><p>Выбранная модель формулирует ответ…</p></div>;
}

function ResultState({ response, provider, model, temperature }: Extract<ViewState, { status: "success" }>) {
  const metrics = response.metrics;
  return <div>
    <div className={styles.answerHeading}><h2>Ответ</h2><p>{providerLabels[provider]} · {modelLabel(provider, model)} · {temperature}</p></div>
    <MarkdownContent className={styles.answer}>{response.answer}</MarkdownContent>
    <dl className={styles.requestMetrics} aria-label="Метрики запроса">
      <div><dt>Время ответа</dt><dd>{formatDuration(metrics.duration_ms)}</dd></div>
      <div><dt>Входные токены</dt><dd>{metrics.input_tokens.toLocaleString("ru-RU")}</dd></div>
      <div><dt>Выходные токены</dt><dd>{metrics.output_tokens.toLocaleString("ru-RU")}</dd></div>
      <div><dt>Стоимость</dt><dd>${formatCost(metrics.cost_usd)}</dd></div>
    </dl>
    <p className={styles.hint}>Расчётная стоимость по тарифам провайдера, без налогов.</p>
  </div>;
}

function formatDuration(milliseconds: number): string {
  return milliseconds < 1000 ? `${milliseconds} мс` : `${(milliseconds / 1000).toFixed(2)} с`;
}

function formatCost(value: number): string {
  return value === 0 ? "0" : value.toFixed(6);
}

function ErrorState({ message, onRetry, disabled = false }: { message: string; onRetry: () => void; disabled?: boolean }) {
  return <div className={styles.errorState} role="alert"><p className={styles.eyebrow}>Не получилось</p><h2>Ответ не получен</h2><p>{message}</p><p>Стоимость неуспешного запроса неизвестна. Повтор создаст новый запрос.</p><button type="button" disabled={disabled} className={styles.retryButton} onClick={onRetry}>Повторить</button></div>;
}
