import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import styles from "./markdown-content.module.css";

type MarkdownContentProps = {
  children: string;
  className?: string;
};

function isSafeLink(href: string) {
  return href.startsWith("/") || /^(https?:|mailto:)/i.test(href);
}

export function MarkdownContent({ children, className }: MarkdownContentProps) {
  return (
    <div className={[styles.content, className].filter(Boolean).join(" ")}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ href, children: linkChildren }) => {
            if (!href || !isSafeLink(href)) return <span>{linkChildren}</span>;
            return (
              <a href={href} target="_blank" rel="noopener noreferrer">
                {linkChildren}
              </a>
            );
          },
          img: () => null,
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
