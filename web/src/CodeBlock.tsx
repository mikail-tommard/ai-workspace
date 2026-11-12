import { Fragment } from "react";
import { CopyButton } from "./CopyButton";
// Lightweight lexical highlighting: escaped React text, never HTML injection.
export default function CodeBlock({
  code,
  language,
}: {
  code: string;
  language: string;
}) {
  const tokens = code.split(
    /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\/\/[^\n]*|#[^\n]*|\b(?:func|package|import|return|const|let|var|function|if|else|for|type|struct|class|def|from|true|false|null|nil|async|await|export|interface)\b|\b\d+(?:\.\d+)?\b)/g,
  );
  return (
    <div className="code-widget">
      <div className="code-toolbar">
        <span className="mono">{language}</span>
        <CopyButton text={code} />
      </div>
      <pre>
        <code>
          {tokens.map((t, i) => {
            const kind = /^["']/.test(t)
              ? "string"
              : /^(\/\/|#)/.test(t)
                ? "comment"
                : /^\d/.test(t)
                  ? "number"
                  : /^\w+$/.test(t)
                    ? "keyword"
                    : "";
            return (
              <Fragment key={i}>
                {i % 2 ? <span className={`syntax-${kind}`}>{t}</span> : t}
              </Fragment>
            );
          })}
        </code>
      </pre>
    </div>
  );
}
