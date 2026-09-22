import Link from "next/link";
import { BaristaWorkspace } from "@/features/barista/components/barista-workspace";
import styles from "./page.module.css";

export default function Home() {
  return (
    <main className={styles.pageShell}>
      <nav className={styles.navigation} aria-label="Основная навигация">
        <Link href="/" aria-current="page">AI-бариста</Link>
        <Link href="/mcp">MCP</Link>
      </nav>
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
