import { Fragment, type ReactNode } from "react";
function inline(s: string): ReactNode[] {
  return s
    .split(/(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*|\[[^\]]+\]\([^\s)]+\))/g)
    .map((part, i) => {
      if (part.startsWith("**") && part.endsWith("**"))
        return <strong key={i}>{part.slice(2, -2)}</strong>;
      if (part.startsWith("`") && part.endsWith("`"))
        return <code key={i}>{part.slice(1, -1)}</code>;
      if (part.startsWith("*") && part.endsWith("*"))
        return <em key={i}>{part.slice(1, -1)}</em>;
      const link = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      if (link) {
        try {
          const u = new URL(link[2], location.href);
          if (["http:", "https:"].includes(u.protocol))
            return (
              <a
                key={i}
                href={u.href}
                target="_blank"
                rel="noreferrer noopener"
              >
                {link[1]}
              </a>
            );
        } catch {
          /* Render invalid URL as inert text. */
        }
      }
      return <Fragment key={i}>{part}</Fragment>;
    });
}
export default function Markdown({ content }: { content: string }) {
  const lines = content.split("\n"),
    nodes: ReactNode[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.startsWith("```")) {
      const code = [];
      while (++i < lines.length && !lines[i].startsWith("```"))
        code.push(lines[i]);
      nodes.push(
        <pre key={i}>
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    if (!line.trim()) continue;
    const heading = line.match(/^(#{1,6}) (.*)$/);
    if (heading) {
      nodes.push(
        heading[1].length <= 2 ? (
          <h3 key={i}>{inline(heading[2])}</h3>
        ) : (
          <h4 key={i}>{inline(heading[2])}</h4>
        ),
      );
      continue;
    }
    if (/^[-*] /.test(line)) {
      const items = [line.slice(2)];
      while (i + 1 < lines.length && /^[-*] /.test(lines[i + 1]))
        items.push(lines[++i].slice(2));
      nodes.push(
        <ul key={i}>
          {items.map((x, j) => (
            <li key={j}>{inline(x)}</li>
          ))}
        </ul>,
      );
      continue;
    }
    if (line.startsWith("> ")) {
      nodes.push(<blockquote key={i}>{inline(line.slice(2))}</blockquote>);
      continue;
    }
    nodes.push(<p key={i}>{inline(line)}</p>);
  }
  return <div className="markdown">{nodes}</div>;
}
