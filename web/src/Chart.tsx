import { useId, useState } from "react";
import type { ChartConfig, Point } from "./types";
const colors = [
  "#358a70",
  "#598ab3",
  "#d7a45c",
  "#a585af",
  "#ca7664",
  "#849966",
];
export default function Chart({ config }: { config: ChartConfig }) {
  const id = useId(),
    [hover, setHover] = useState<Point | null>(null),
    points = config.points;
  const xs = points.map((p) => p.x),
    ys = points.map((p) => p.y),
    minX = Math.min(...xs),
    maxX = Math.max(...xs),
    minY = Math.min(0, ...ys),
    maxY = Math.max(0, ...ys);
  const X = (x: number) => 48 + ((x - minX) / (maxX - minX || 1)) * 474,
    Y = (y: number) => 230 - ((y - minY) / (maxY - minY || 1)) * 202;
  let angle = -Math.PI / 2;
  const sum = ys.reduce((a, b) => a + b, 0);
  function tip(p: Point) {
    setHover(p);
  }
  return (
    <div className="chart">
      <svg
        viewBox="0 0 560 280"
        role="img"
        aria-labelledby={id}
        onMouseLeave={() => setHover(null)}
      >
        <title id={id}>
          {config.kind} график: {config.x_label || "x"}, {config.y_label || "y"}
        </title>
        {config.kind === "pie" ? (
          points.map((p, i) => {
            const start = angle;
            angle += (p.y / sum) * Math.PI * 2;
            const end = angle,
              cx = 280,
              cy = 137,
              r = 108;
            const d =
              p.y === sum
                ? `M ${cx - r},${cy} a ${r},${r} 0 1,0 ${2 * r},0 a ${r},${r} 0 1,0 ${-2 * r},0`
                : `M${cx},${cy} L${cx + r * Math.cos(start)},${cy + r * Math.sin(start)} A${r},${r} 0 ${end - start > Math.PI ? 1 : 0},1 ${cx + r * Math.cos(end)},${cy + r * Math.sin(end)} Z`;
            return (
              <path
                key={i}
                d={d}
                fill={colors[i % colors.length]}
                tabIndex={0}
                aria-label={`${p.label || p.x}: ${p.y}`}
                onFocus={() => tip(p)}
                onMouseEnter={() => tip(p)}
              >
                <title>
                  {p.label || p.x}: {p.y}
                </title>
              </path>
            );
          })
        ) : (
          <>
            {[0, 1, 2, 3, 4].map((i) => {
              const v = minY + ((maxY - minY) * i) / 4;
              return (
                <g key={i}>
                  <line
                    x1="48"
                    x2="522"
                    y1={Y(v)}
                    y2={Y(v)}
                    className="gridline"
                  />
                  <text x="37" y={Y(v) + 4} textAnchor="end">
                    {Number(v.toFixed(2))}
                  </text>
                </g>
              );
            })}
            <line x1="48" x2="522" y1={Y(0)} y2={Y(0)} className="axis" />
            {[minX, (minX + maxX) / 2, maxX].map((x, i) => (
              <text key={i} x={X(x)} y="253" textAnchor="middle">
                {Number(x.toFixed(2))}
              </text>
            ))}
            {config.kind === "line" ? (
              <polyline
                points={points.map((p) => `${X(p.x)},${Y(p.y)}`).join(" ")}
                fill="none"
                stroke={colors[0]}
                strokeWidth="3"
              />
            ) : null}
            {points.map((p, i) =>
              config.kind === "bar" ? (
                <rect
                  key={i}
                  x={X(p.x) - Math.min(20, 180 / points.length)}
                  y={Math.min(Y(p.y), Y(0))}
                  width={Math.min(40, 360 / points.length)}
                  height={Math.max(1, Math.abs(Y(p.y) - Y(0)))}
                  rx="3"
                  fill={colors[0]}
                  tabIndex={0}
                  onFocus={() => tip(p)}
                  onMouseEnter={() => tip(p)}
                  aria-label={`${p.label || p.x}: ${p.y}`}
                >
                  <title>
                    {p.label || p.x}: {p.y}
                  </title>
                </rect>
              ) : (
                <circle
                  key={i}
                  cx={X(p.x)}
                  cy={Y(p.y)}
                  r={config.kind === "scatter" ? 5 : 3.5}
                  fill={colors[0]}
                  tabIndex={0}
                  onFocus={() => tip(p)}
                  onMouseEnter={() => tip(p)}
                  aria-label={`${p.x}: ${p.y}`}
                >
                  <title>
                    {p.x}: {p.y}
                  </title>
                </circle>
              ),
            )}
          </>
        )}
        <text x="280" y="275" textAnchor="middle">
          {config.x_label || "x"}
        </text>
      </svg>
      <div className="chart-caption" aria-live="polite">
        {hover ? (
          <>
            <strong>{hover.label || `x = ${hover.x}`}</strong>
            <span>y = {hover.y}</span>
          </>
        ) : (
          <>
            <span>{points.length} точек</span>
            <span>Наведите на график для подробностей</span>
          </>
        )}
      </div>
      <details className="chart-data">
        <summary>Данные графика</summary>
        <table>
          <thead>
            <tr>
              <th>{config.x_label || "x"}</th>
              <th>{config.y_label || "y"}</th>
            </tr>
          </thead>
          <tbody>
            {points.map((p, i) => (
              <tr key={i}>
                <td>{p.label || p.x}</td>
                <td>{p.y}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </div>
  );
}
