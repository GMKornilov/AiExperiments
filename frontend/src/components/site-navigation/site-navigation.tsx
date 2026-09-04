import Link from "next/link";
import styles from "./site-navigation.module.css";

export function SiteNavigation({ active }: { active: "barista" | "algorithms" | "temperature" }) {
  return <nav className={styles.navigation} aria-label="Основная навигация">
    <Link aria-label="Бариста, Неделя 1, Задание 2" className={active === "barista" ? styles.active : styles.link} aria-current={active === "barista" ? "page" : undefined} href="/">
      <span className={styles.title}>Бариста</span>
      <span className={styles.subtitle}>Неделя 1, Задание 2</span>
    </Link>
    <Link aria-label="Алгоритмы, Неделя 1, Задание 3" className={active === "algorithms" ? styles.active : styles.link} aria-current={active === "algorithms" ? "page" : undefined} href="/algorithms">
      <span className={styles.title}>Алгоритмы</span>
      <span className={styles.subtitle}>Неделя 1, Задание 3</span>
    </Link>
    <Link aria-label="Температура, Неделя 1, задание 4" className={active === "temperature" ? styles.active : styles.link} aria-current={active === "temperature" ? "page" : undefined} href="/temperature">
      <span className={styles.title}>Температура</span>
      <span className={styles.subtitle}>Неделя 1, задание 4</span>
    </Link>
  </nav>;
}
