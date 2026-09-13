import type { CompressionState } from "../model/types";
import styles from "./context-meter.module.css";

export function ContextMeter({ state }: { state?: CompressionState }) {
  const input = state?.last_input_tokens ?? null;
  const limit = state?.context_window_tokens ?? 0;
  const percent = input !== null && limit > 0 ? Math.min(100, input / limit * 100) : 0;
  return <section className={styles.panel} aria-label="Статистика контекста">
    <div className={styles.heading}><span>Контекст</span><strong>{input === null ? "Нет данных о входе" : `${input.toLocaleString("ru-RU")} токенов`}{limit > 0 && ` / ${limit.toLocaleString("ru-RU")}`}</strong></div>
    {limit > 0 ? <div className={styles.track} role="progressbar" aria-label="Заполнение контекстного окна" aria-valuemin={0} aria-valuemax={limit} aria-valuenow={input === null ? undefined : Math.min(input, limit)} aria-valuetext={input === null ? "Нет данных" : `${input} из ${limit} токенов`}>
      <div className={styles.fill} style={{ width: `${percent}%`, backgroundColor: percent >= 90 ? "#b84b35" : undefined }} />
    </div> : <small>Лимит контекстного окна не настроен.</small>}
    {input !== null && limit > 0 && <small>{(input / limit * 100).toFixed(1)}% окна · вход по статистике провайдера</small>}
    <details className={styles.more}><summary>Статистика запроса</summary>
    {!!state?.summary && <small>После /compact размер обновится со следующим запросом к ассистенту.</small>}
    {!!state?.full_estimate && <small>Оценка входа (UTF-8 / 4): до сжатия ≈ {state.full_estimate.toLocaleString("ru-RU")} → отправлено ≈ {state.sent_estimate.toLocaleString("ru-RU")} токенов</small>}
    {!!state?.available && <small>Суммаризация: {state.summary_tokens.toLocaleString("ru-RU")} учтённых токенов{state.summary_usage_missing && " · часть расхода неизвестна"} · заменено на summary сообщений: {state.covered_messages}</small>}
    </details>
    {state?.summary && <details><summary>Посмотреть summary</summary><p className={styles.summary}>{state.summary}</p></details>}
  </section>;
}
