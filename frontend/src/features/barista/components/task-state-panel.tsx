"use client";

import type { Task, TaskCandidate, TaskPlanItem } from "../model/types";
import styles from "./task-state-panel.module.css";

const stageNames = {
  clarify_input: "Уточняем запрос",
  research_input_data: "Собираем данные",
  execution: "Выполняем задачу",
  user_feedback: "Ждём обратную связь",
} as const;

const statusNames = { active: "В работе", paused: "На паузе", done: "Завершена" } as const;
const planStateNames = { completed: "Готово", current: "В работе", pending: "Предстоит" } as const;

type Props = {
  tasks: Task[];
  candidates: TaskCandidate[];
  pending: boolean;
  onCandidate: (candidate: TaskCandidate) => void;
  detailsID: string;
  open: boolean;
};

export const taskStatusSummary = (tasks: Task[]) => {
  const active = tasks.filter(task => task.status === "active").length;
  const paused = tasks.filter(task => task.status === "paused").length;
  if (active > 0) return `${active} в работе`;
  if (paused > 0) return `${paused} на паузе`;
  if (tasks.length > 0) return `${tasks.length} завершено`;
  return "нет задач";
};

const planState = (task: Task, item: TaskPlanItem): TaskPlanItem["status"] => task.status === "done" ? "completed" : item.status;

export function TaskStatePanel({ tasks, candidates, pending, onCandidate, detailsID, open }: Props) {
  const detailsOpen = open || candidates.length > 0;
  if (!detailsOpen) return null;

  return <section className={styles.panel} aria-label="Состояние задач">
    <div id={detailsID} className={styles.details}>
      {candidates.length > 0 && <section className={styles.candidates} aria-labelledby="task-candidates-title">
      <h3 id="task-candidates-title">К какой задаче относится сообщение?</h3>
      <p>Выберите задачу, чтобы продолжить её. Агент не будет выбирать за вас.</p>
      <ul>{candidates.map(candidate => <li key={candidate.id}><button type="button" disabled={pending} onClick={() => onCandidate(candidate)}><strong>{candidate.title}</strong><span>{candidate.description}</span></button></li>)}</ul>
    </section>}
    {tasks.length === 0 ? <p className={styles.empty}>Задачи появятся после первого запроса.</p> : <ul className={styles.list}>{tasks.map(task => <li key={task.id} className={styles.card}>
      <div className={styles.heading}><h3>{task.title}</h3><span className={`${styles.status} ${styles[task.status]}`}>{statusNames[task.status]}</span></div>
      <p>{task.description}</p>
      <p className={styles.stage}>Этап: {stageNames[task.stage]}</p>
      {(task.plan ?? []).length === 0 ? <div className={styles.planEmpty}>
        <p>План появится после подтверждения цели.</p>
        <dl className={styles.stepDetails}>
          <div><dt>Текущий вопрос</dt><dd>{task.current_step}</dd></div>
          <div><dt>Ожидаемое действие</dt><dd>{task.expected_action === "none" ? "Нет" : task.expected_action}</dd></div>
        </dl>
      </div> : <ol className={styles.plan} aria-label={`План задачи «${task.title}»`}>{(task.plan ?? []).map(item => {
        const state = planState(task, item);
        const isCurrent = item.id === task.current_plan_item && state === "current";
        const stateLabel = isCurrent && task.status === "paused" ? "На паузе" : planStateNames[state];
        return <li key={item.id} className={`${styles.planStep} ${styles[state]}`} aria-current={isCurrent ? "step" : undefined}>
          <div className={styles.planHeading}><span>{item.title}</span><span className={styles.planState}>{stateLabel}</span></div>
          {isCurrent && <dl className={styles.stepDetails}>
            <div><dt>Текущий шаг</dt><dd>{task.current_step}</dd></div>
            <div><dt>Ожидаемое действие</dt><dd>{task.expected_action === "none" ? "Нет" : task.expected_action}</dd></div>
          </dl>}
          {isCurrent && task.status === "paused" && <p className={styles.resumeHint}>Продолжите задачу кнопкой в поле ввода.</p>}
        </li>;
      })}</ol>}
      {task.stage === "user_feedback" && task.status !== "done" && <p className={styles.feedbackHint}>Задача ожидает вашу обратную связь.</p>}
      {task.status === "done" && <p className={styles.doneHint}>Задача завершена.</p>}
    </li>)}</ul>}
    </div>
  </section>;
}
