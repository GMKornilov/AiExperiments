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
          Спросите о зёрнах, помоле, рецепте или о том, как улучшить чашку.
        </p>
      </header>

      <BaristaWorkspace />

      <footer className={styles.footer}>Тихий помол · AI-бариста · <a href="/admin">Журнал</a></footer>
    </main>
  );
}
