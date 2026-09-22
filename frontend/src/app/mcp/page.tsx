import Link from "next/link";
import { MCPWorkspace } from "@/features/mcp/components/mcp-workspace";
import styles from "./page.module.css";

export default function MCPPage() {
  return (
    <main className={styles.pageShell}>
      <nav className={styles.navigation} aria-label="Основная навигация">
        <Link href="/">AI-бариста</Link>
        <Link href="/mcp" aria-current="page">MCP</Link>
      </nav>
      <MCPWorkspace />
    </main>
  );
}
