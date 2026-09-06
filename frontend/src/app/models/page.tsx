import type { Metadata } from "next";
import { SiteNavigation } from "@/components/site-navigation/site-navigation";
import { ModelTemperatureWorkspace } from "@/features/model-temperature/components/model-temperature-workspace";
import styles from "../page.module.css";
import comparisonStyles from "@/features/model-temperature/components/model-comparison.module.css";

export const metadata: Metadata = {
  title: "Модели — Тихий помол",
  description: "Сравнение ответов DeepSeek V4 Flash, V4 Pro и Kimi K3 с метриками.",
};

export default function ModelsPage() {
  return <main className={`${styles.pageShell} ${comparisonStyles.page}`}>
    <SiteNavigation active="models" />
    <header className={styles.masthead}>
      <p className={styles.eyebrow}>Управление параметрами запроса</p>
      <h1 className={styles.title}>Модели<span aria-hidden="true">.</span></h1>
      <p className={styles.intro}>Отправьте один промпт в три модели сразу и сравните ответы, время, токены и стоимость.</p>
    </header>
    <ModelTemperatureWorkspace />
    <footer className={styles.footer}>Тихий помол · Модели</footer>
  </main>;
}
