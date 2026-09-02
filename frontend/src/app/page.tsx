import { BaristaWorkspace } from "@/features/barista/components/barista-workspace";
import styles from "./page.module.css";

export default function Home() {
  return (
    <main className={styles.pageShell}>
      <header className={styles.masthead}>
        <p className={styles.eyebrow}>Ваш кофейный напарник</p>
        <h1 className={styles.title}>
          Тихий помол<span aria-hidden="true">.</span>
        </h1>
        <p className={styles.intro}>
          Настройте рецепт, разберите вкус или сравните свободный ответ со
          структурированным JSON.
        </p>
      </header>

      <BaristaWorkspace />

      <footer className={styles.footer}>Тихий помол · AI-бариста</footer>
    </main>
  );
}
