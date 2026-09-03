import type { Metadata } from "next";
import { SiteNavigation } from "@/components/site-navigation/site-navigation";
import { AlgorithmsWorkspace } from "@/features/algorithms/components/algorithms-workspace";
import styles from "../page.module.css";

export const metadata: Metadata = {
  title: "Алгоритмы — Тихий помол",
  description: "Четыре независимых prompt-подхода для решения алгоритмических задач.",
  openGraph: {
    title: "Алгоритмы — Тихий помол",
    description: "Четыре независимых prompt-подхода для решения алгоритмических задач.",
    images: [],
  },
  twitter: {
    title: "Алгоритмы — Тихий помол",
    description: "Четыре независимых prompt-подхода для решения алгоритмических задач.",
    images: [],
  },
};

export default function AlgorithmsPage() {
  return <main className={styles.pageShell}>
    <SiteNavigation active="algorithms" />
    <header className={styles.masthead}>
      <p className={styles.eyebrow}>Ручное сопоставление prompt-подходов</p>
      <h1 className={styles.title}>Алгоритмы<span aria-hidden="true">.</span></h1>
      <p className={styles.intro}>Получите четыре независимых решения одной задачи и изучите фактически отправленные prompts.</p>
    </header>
    <AlgorithmsWorkspace />
    <footer className={styles.footer}>Тихий помол · Алгоритмы</footer>
  </main>;
}
