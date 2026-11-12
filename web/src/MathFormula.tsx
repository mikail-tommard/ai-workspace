import { createElement as h, type ReactNode } from "react";
// A bounded, safe LaTeX subset for the deterministic generator. It constructs
// MathML nodes rather than HTML and never runs macros or user scripts.
function parse(source: string): ReactNode[] {
  let i = 0,
    key = 0;
  function group(): ReactNode[] {
    if (source[i] === "{") {
      i++;
      return sequence("}");
    }
    const ch = source[i++] || "";
    return [h(/\d/.test(ch) ? "mn" : "mi", { key: key++ }, ch)];
  }
  function sequence(end?: string): ReactNode[] {
    const out: ReactNode[] = [];
    while (i < source.length) {
      const c = source[i++];
      if (c === end) break;
      if (c === " ") {
        continue;
      }
      if (c === "\\") {
        const match = /^[a-zA-Z]+/.exec(source.slice(i));
        const cmd = match?.[0] || source[i++] || "";
        if (match) i += cmd.length;
        if (cmd === "frac") {
          const numerator = group(),
            denominator = group();
          out.push(
            h(
              "mfrac",
              { key: key++ },
              h("mrow", null, ...numerator),
              h("mrow", null, ...denominator),
            ),
          );
        } else if (cmd === "text") {
          if (source[i] === "{") {
            i++;
            const end = source.indexOf("}", i);
            out.push(
              h(
                "mtext",
                { key: key++ },
                source.slice(i, end < 0 ? source.length : end),
              ),
            );
            i = end < 0 ? source.length : end + 1;
          }
        } else
          out.push(
            h(
              "mo",
              { key: key++ },
              ({ times: "×", "%": "%", cdot: "·" } as Record<string, string>)[
                cmd
              ] || cmd,
            ),
          );
        continue;
      }
      if (c === "^") {
        const base = out.pop();
        out.push(h("msup", { key: key++ }, base, h("mrow", null, ...group())));
        continue;
      }
      if (c === "{") {
        out.push(h("mrow", { key: key++ }, ...sequence("}")));
        continue;
      }
      if (/\d/.test(c)) {
        let text = c;
        while (i < source.length && /[\d.]/.test(source[i]))
          text += source[i++];
        out.push(h("mn", { key: key++ }, text));
        continue;
      }
      out.push(h(/[a-zA-Zа-яА-Я]/.test(c) ? "mi" : "mo", { key: key++ }, c));
    }
    return out;
  }
  return sequence();
}
export default function MathFormula({ latex }: { latex: string }) {
  const cases = latex.match(/^\\begin\{cases\}([\s\S]*)\\end\{cases\}$/);
  return (
    <div className="formula" aria-label={latex}>
      {h(
        "math",
        { xmlns: "http://www.w3.org/1998/Math/MathML", display: "block" },
        cases
          ? h(
              "mrow",
              null,
              h("mo", null, "{"),
              h(
                "mtable",
                null,
                ...cases[1]
                  .split("\\\\")
                  .map((line, i) =>
                    h("mtr", { key: i }, h("mtd", null, ...parse(line))),
                  ),
              ),
            )
          : h("mrow", null, ...parse(latex)),
      )}
    </div>
  );
}
