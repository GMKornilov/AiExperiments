"use client";

import { memo, useEffect, useId, useRef, useState } from "react";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import styles from "./collapsible-message.module.css";

const chunkSize = 20_000;

export const CollapsibleMessage = memo(function CollapsibleMessage({ text }: { text: string }) {
  const id = useId();
  const content = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflowing, setOverflowing] = useState(false);
  const [page, setPage] = useState(0);
  const large = text.length > chunkSize;
  const pages = Math.ceil(text.length / chunkSize);

  useEffect(() => {
    const element = content.current;
    if (!element) return;
    const measure = () => {
      const lineHeight = Number.parseFloat(getComputedStyle(element).lineHeight);
      setOverflowing(element.getBoundingClientRect().height > lineHeight * 5 + 1);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [text]);

  return <div className={styles.message}>
    <div id={id} className={!expanded ? styles.collapsed : undefined}>
      <div ref={content}>{large
        ? <div className={styles.plain}>{expanded ? text.slice(page * chunkSize, (page + 1) * chunkSize) : text.slice(0, 1000)}</div>
        : <MarkdownContent>{text}</MarkdownContent>}</div>
    </div>
    {large && expanded && <nav aria-label="Фрагменты сообщения">
      <button type="button" disabled={page === 0} onClick={() => setPage((value) => value - 1)}>Предыдущий фрагмент</button>
      <span> Фрагмент {page + 1} из {pages} </span>
      <button type="button" disabled={page >= pages - 1} onClick={() => setPage((value) => value + 1)}>Следующий фрагмент</button>
    </nav>}
    {(large || overflowing) && <button type="button" className={styles.toggle} aria-expanded={expanded} aria-controls={id} onClick={() => { setExpanded((value) => !value); setPage(0); }}>
      {expanded ? "Свернуть" : "Развернуть"}
    </button>}
  </div>;
});
