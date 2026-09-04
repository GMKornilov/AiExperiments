import type { Metadata } from "next";
import { SiteNavigation } from "@/components/site-navigation/site-navigation";
import { TemperatureWorkspace } from "@/features/temperature/components/temperature-workspace";
import styles from "../page.module.css";

export const metadata: Metadata = {
  title: "Температура — Тихий помол",
  description: "Один prompt с настраиваемой температурой генерации.",
};

export default function TemperaturePage() {
  return <main className={styles.pageShell}>
    <SiteNavigation active="temperature" />
    <header className={styles.masthead}>
      <p className={styles.eyebrow}>Вариативность одного запроса</p>
      <h1 className={styles.title}>Температура<span aria-hidden="true">.</span></h1>
      <p className={styles.intro}>Выберите температуру и сравните, как она влияет на один самостоятельный ответ модели.</p>
    </header>
    <TemperatureWorkspace />
    <footer className={styles.footer}>Тихий помол · Температура</footer>
  </main>;
}
