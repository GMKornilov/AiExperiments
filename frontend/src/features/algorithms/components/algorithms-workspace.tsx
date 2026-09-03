"use client";

import { FormEvent, KeyboardEvent, useId, useRef, useState } from "react";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import { algorithmMethodLabels, algorithmMethods, type AlgorithmMethod, useAlgorithms } from "../algorithms-store";
import styles from "./algorithms-workspace.module.css";

const statusLabels = { idle: "Ожидает запуска", loading: "Загрузка", success: "Готово", error: "Ошибка" } as const;

export function AlgorithmsWorkspace() {
  const { statement, language, results, activeMethod, isPending, setStatement, setLanguage, setActiveMethod, run } = useAlgorithms();
  const [validationError, setValidationError] = useState("");
  const tabRefs = useRef<Partial<Record<AlgorithmMethod, HTMLButtonElement | null>>>({});
  const tabsId = useId();
  const codePointCount = Array.from(statement).length;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!statement.trim()) {
      setValidationError("Введите условие задачи.");
      return;
    }
    if (codePointCount > 10_000) {
      setValidationError("Условие задачи должно содержать не более 10 000 символов.");
      return;
    }
    setValidationError("");
    run();
  }

  function onTabKeyDown(event: KeyboardEvent<HTMLButtonElement>, method: AlgorithmMethod) {
    const current = algorithmMethods.indexOf(method);
    let next: number | undefined;
    if (event.key === "ArrowRight" || event.key === "ArrowDown") next = (current + 1) % algorithmMethods.length;
    if (event.key === "ArrowLeft" || event.key === "ArrowUp") next = (current - 1 + algorithmMethods.length) % algorithmMethods.length;
    if (event.key === "Home") next = 0;
    if (event.key === "End") next = algorithmMethods.length - 1;
    if (next === undefined) return;
    event.preventDefault();
    const nextMethod = algorithmMethods[next];
    setActiveMethod(nextMethod);
    tabRefs.current[nextMethod]?.focus();
  }

  const activeResult = results[activeMethod];
  return (
    <div className={styles.workspace}>
      <section className={styles.composer} aria-labelledby="algorithm-form-title">
        <div className={styles.sectionHeading}>
          <h2 id="algorithm-form-title">Сравните подходы</h2>
          <p>Одна задача, четыре независимых способа получить решение.</p>
        </div>
        <form onSubmit={submit} noValidate>
          <label className={styles.fieldLabel} htmlFor="algorithm-statement">Условие задачи</label>
          <textarea id="algorithm-statement" name="statement" rows={9} value={statement} onChange={(event) => setStatement(event.target.value)} aria-describedby="statement-hint statement-error" aria-invalid={Boolean(validationError)} placeholder="Вставьте условие, форматы входа и результата, ограничения и примеры." />
          <div className={styles.fieldMeta}>
            <p id="statement-hint">Условие передаётся целиком, включая пробелы и переводы строк.</p>
            <span>{codePointCount} / 10 000</span>
          </div>
          {validationError && <p id="statement-error" className={styles.validationError} role="alert">{validationError}</p>}

          <label className={styles.fieldLabel} htmlFor="algorithm-language">Язык</label>
          <select id="algorithm-language" name="language" value={language} onChange={(event) => setLanguage(event.target.value as typeof language)}>
            <option value="python">Python 3</option>
            <option value="java">Java</option>
            <option value="cpp">C++</option>
          </select>

          <div className={styles.formFooter}>
            <p className={styles.hint}>Enter добавляет новую строку</p>
            <button type="submit" disabled={isPending}><span>{isPending ? "Получаем решения…" : "Получить решения"}</span><span aria-hidden="true">→</span></button>
          </div>
        </form>
      </section>

      <section className={styles.responsePanel} aria-live="polite" aria-busy={isPending} aria-label="Результаты алгоритмов">
        <div className={styles.tabs} role="tablist" aria-label="Способы решения">
          {algorithmMethods.map((method) => {
            const result = results[method];
            const selected = activeMethod === method;
            return <button key={method} ref={(element) => { tabRefs.current[method] = element; }} id={`${tabsId}-${method}-tab`} role="tab" type="button" aria-selected={selected} aria-controls={`${tabsId}-panel`} tabIndex={selected ? 0 : -1} className={selected ? styles.activeTab : styles.tab} onClick={() => setActiveMethod(method)} onKeyDown={(event) => onTabKeyDown(event, method)}>
              <span>{algorithmMethodLabels[method]}</span><small>{statusLabels[result.status]}</small>
            </button>;
          })}
        </div>
        <div id={`${tabsId}-panel`} role="tabpanel" aria-labelledby={`${tabsId}-${activeMethod}-tab`} className={styles.result} tabIndex={0}>
          <ResultContent result={activeResult} />
        </div>
      </section>
    </div>
  );
}

function ResultContent({ result }: { result: ReturnType<typeof useAlgorithms>["results"][AlgorithmMethod] }) {
  if (result.status === "idle") return <div className={styles.emptyState}><p className={styles.emptyMark} aria-hidden="true">01</p><h2>Задача ждёт запуска</h2><p>Запустите все четыре подхода одной кнопкой.</p></div>;
  if (result.status === "loading") return <div className={styles.loadingState} role="status"><span className={styles.loader} aria-hidden="true" /><p>Метод получает решение…</p></div>;
  return <div>
    <div className={styles.answerHeading}><h2>{algorithmMethodLabels[result.method]}</h2><p>{statusLabels[result.status]}</p></div>
    {result.answer && <MarkdownContent className={styles.answer}>{result.answer}</MarkdownContent>}
    {result.status === "error" && <div className={styles.errorState} role="alert"><p className={styles.eyebrow}>Не получилось</p><p>{result.error}</p></div>}
    {result.method === "generated-prompt" && result.trace[0]?.response && <section className={styles.intermediate} aria-label="Промежуточный prompt"><p className={styles.sectionLabel}>Сгенерированный prompt</p><MarkdownContent>{result.trace[0].response}</MarkdownContent></section>}
    <TraceView trace={result.trace} />
  </div>;
}

function TraceView({ trace }: { trace: ReturnType<typeof useAlgorithms>["results"][AlgorithmMethod]["trace"] }) {
  return <details className={styles.rawPrompts}><summary>Сырые промпты ({trace.length}) <span aria-hidden="true">⌄</span></summary>
    {trace.map((step, index) => <div className={styles.traceStep} key={`${step.step}-${index}`}><p>Вызов {index + 1}: {step.step === "generate-prompt" ? "создание prompt" : "решение"}</p>{step.messages.map((message, messageIndex) => <div key={`${message.role}-${messageIndex}`}><strong>{message.role}</strong><pre>{message.content}</pre></div>)}</div>)}
  </details>;
}
