import { useEffect, useId, useMemo, useRef, useState, type PointerEvent } from "react";
import { motion } from "framer-motion";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { goldHistoryApi, type GoldHistory } from "@/lib/api";
import { cn } from "@/lib/utils";

type Period = "1M" | "3M" | "6M" | "1Y";
const PERIODS: Record<Period, number> = { "1M": 31, "3M": 92, "6M": 183, "1Y": 366 };
const HEIGHT = 220;
const M = { top: 12, right: 12, bottom: 26, left: 64 };

const fmt = (v: number, d = 0) =>
  new Intl.NumberFormat("ru-RU", { minimumFractionDigits: d, maximumFractionDigits: d }).format(v);
const dayFmt = new Intl.DateTimeFormat("en", { day: "numeric", month: "short" });
const monthFmt = new Intl.DateTimeFormat("en", { month: "short" });
const fullFmt = new Intl.DateTimeFormat("en", { day: "numeric", month: "short", year: "numeric" });
const toTime = (d: string) => new Date(d + "T12:00:00").getTime();

/** "Nice" round tick values covering [min, max]. */
function niceTicks(min: number, max: number, count = 4): number[] {
  const raw = (max - min) / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw;
  const out: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max; v += step) out.push(v);
  return out;
}

export function GoldChart() {
  const [data, setData] = useState<GoldHistory | null>(null);
  const [error, setError] = useState(false);
  const [period, setPeriod] = useState<Period>("3M");
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | null>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const gradientId = useId();

  useEffect(() => {
    goldHistoryApi.get().then(setData).catch(() => setError(true));
  }, []);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(e.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const points = useMemo(() => {
    if (!data) return [];
    const cutoff = Date.now() - PERIODS[period] * 86_400_000;
    return data.points.filter((p) => toTime(p.date) >= cutoff).map((p) => ({ ...p, t: toTime(p.date) }));
  }, [data, period]);

  const geo = useMemo(() => {
    if (points.length < 2 || width < 50) return null;
    const values = points.map((p) => p.value);
    const lo = Math.min(...values), hi = Math.max(...values);
    const pad = (hi - lo || hi * 0.01) * 0.12;
    const yMin = lo - pad, yMax = hi + pad;
    const t0 = points[0].t, t1 = points[points.length - 1].t;
    const iw = width - M.left - M.right, ih = HEIGHT - M.top - M.bottom;
    const x = (t: number) => M.left + ((t - t0) / (t1 - t0 || 1)) * iw;
    const y = (v: number) => M.top + (1 - (v - yMin) / (yMax - yMin)) * ih;
    const line = points.map((p, i) => `${i ? "L" : "M"}${x(p.t).toFixed(1)},${y(p.value).toFixed(1)}`).join("");
    const area = `${line}L${x(t1).toFixed(1)},${M.top + ih}L${x(t0).toFixed(1)},${M.top + ih}Z`;
    const yTicks = niceTicks(yMin, yMax);
    const xCount = Math.max(2, Math.min(6, Math.floor(iw / 90)));
    const xTicks = Array.from({ length: xCount }, (_, i) => t0 + ((t1 - t0) * i) / (xCount - 1));
    return { x, y, line, area, yTicks, xTicks, ih, lo, hi };
  }, [points, width]);

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    if (!geo) return;
    const px = e.clientX - e.currentTarget.getBoundingClientRect().left;
    let best = 0, dist = Infinity;
    points.forEach((p, i) => {
      const d = Math.abs(geo.x(p.t) - px);
      if (d < dist) [best, dist] = [i, d];
    });
    setHover(best);
  };

  const first = points[0], last = points[points.length - 1];
  const change = first && last ? last.value - first.value : 0;
  const changePct = first ? (change / first.value) * 100 : 0;
  const active = hover !== null ? points[hover] : null;
  const longRange = PERIODS[period] > 92;

  return (
    <div className="px-5 pb-5 sm:px-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs text-muted-foreground">NBK official price · ₸ per gram</p>
          {last ? (
            <div className="mt-1 flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-2xl font-semibold tabular-nums tracking-tight">{fmt(last.value, 2)} ₸</span>
              <span
                className={cn(
                  "inline-flex items-center gap-0.5 text-sm tabular-nums",
                  change >= 0 ? "text-success" : "text-destructive",
                )}
              >
                {change >= 0 ? <ArrowUpRight className="h-3.5 w-3.5" /> : <ArrowDownRight className="h-3.5 w-3.5" />}
                {change >= 0 ? "+" : ""}
                {fmt(change, 0)} ₸ ({change >= 0 ? "+" : ""}
                {fmt(changePct, 2)}%)
              </span>
              <span className="text-xs text-muted-foreground">over {period}</span>
            </div>
          ) : (
            <Skeleton className="mt-1.5 h-7 w-56" />
          )}
        </div>
        <div role="tablist" aria-label="Period" className="inline-flex rounded-lg border bg-muted/40 p-0.5">
          {(Object.keys(PERIODS) as Period[]).map((p) => (
            <button
              key={p}
              role="tab"
              aria-selected={period === p}
              onClick={() => {
                setPeriod(p);
                setHover(null);
              }}
              className={cn(
                "relative rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                period === p && "text-foreground",
              )}
            >
              {period === p && (
                <motion.span
                  layoutId="gold-period"
                  className="absolute inset-0 rounded-md border bg-background shadow-sm"
                  transition={{ type: "spring", stiffness: 500, damping: 38 }}
                />
              )}
              <span className="relative">{p}</span>
            </button>
          ))}
        </div>
      </div>

      <div ref={wrap} className="relative mt-4" style={{ height: HEIGHT }}>
        {error ? (
          <p className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Price history is temporarily unavailable.
          </p>
        ) : !geo ? (
          <Skeleton className="h-full w-full rounded-lg" />
        ) : (
          <>
            <svg
              width={width}
              height={HEIGHT}
              className="touch-none select-none overflow-visible"
              onPointerMove={onMove}
              onPointerDown={onMove}
              onPointerLeave={() => setHover(null)}
              role="img"
              aria-label={`NBK gold price over ${period}: from ${fmt(first.value)} to ${fmt(last.value)} tenge per gram`}
            >
              <defs>
                <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%" stopColor="var(--chart-gold)" stopOpacity="0.18" />
                  <stop offset="100%" stopColor="var(--chart-gold)" stopOpacity="0" />
                </linearGradient>
              </defs>

              {/* recessive grid + y axis labels */}
              {geo.yTicks.map((v) => (
                <g key={v}>
                  <line
                    x1={M.left}
                    x2={width - M.right}
                    y1={geo.y(v)}
                    y2={geo.y(v)}
                    className="stroke-border"
                    strokeDasharray="2 4"
                  />
                  <text x={M.left - 10} y={geo.y(v)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[11px] tabular-nums">
                    {fmt(v)}
                  </text>
                </g>
              ))}
              {geo.xTicks.map((t, i) => (
                <text
                  key={t}
                  x={geo.x(t)}
                  y={HEIGHT - 6}
                  textAnchor={i === 0 ? "start" : i === geo.xTicks.length - 1 ? "end" : "middle"}
                  className="fill-muted-foreground text-[11px]"
                >
                  {(longRange ? monthFmt : dayFmt).format(t)}
                </text>
              ))}

              <path key={`a-${period}`} d={geo.area} fill={`url(#${gradientId})`} className="animate-in fade-in-0 duration-500" />
              <path
                key={`l-${period}`}
                d={geo.line}
                pathLength={1}
                fill="none"
                stroke="var(--chart-gold)"
                strokeWidth={2}
                strokeLinejoin="round"
                strokeLinecap="round"
                className="chart-draw"
              />

              {/* end-point direct label */}
              {!active && (
                <circle cx={geo.x(last.t)} cy={geo.y(last.value)} r={4} fill="var(--chart-gold)" className="stroke-card" strokeWidth={2} />
              )}

              {active && (
                <g pointerEvents="none">
                  <line x1={geo.x(active.t)} x2={geo.x(active.t)} y1={M.top} y2={M.top + geo.ih} className="stroke-foreground/30" />
                  <circle cx={geo.x(active.t)} cy={geo.y(active.value)} r={4.5} fill="var(--chart-gold)" className="stroke-card" strokeWidth={2} />
                </g>
              )}
            </svg>

            {active && (
              <div
                className="pointer-events-none absolute top-0 z-10 rounded-lg border bg-popover px-3 py-2 text-xs shadow-lg"
                style={{
                  left: Math.min(Math.max(geo.x(active.t) - 70, 0), width - 140),
                  width: 140,
                }}
              >
                <p className="text-muted-foreground">{fullFmt.format(active.t)}</p>
                <p className="mt-0.5 flex items-center gap-1.5 font-medium tabular-nums">
                  <span className="h-2 w-2 rounded-full" style={{ background: "var(--chart-gold)" }} />
                  {fmt(active.value, 2)} ₸
                </p>
              </div>
            )}
          </>
        )}
      </div>

      {points.length > 0 && (
        <details className="mt-2 text-xs text-muted-foreground">
          <summary className="cursor-pointer select-none hover:text-foreground">View as table</summary>
          <div className="mt-2 max-h-48 overflow-auto rounded-md border">
            <table className="w-full tabular-nums">
              <thead className="sticky top-0 bg-muted">
                <tr>
                  <th className="px-3 py-1.5 text-left font-medium">Date</th>
                  <th className="px-3 py-1.5 text-right font-medium">₸ / gram</th>
                </tr>
              </thead>
              <tbody className="divide-y text-foreground">
                {[...points].reverse().map((p) => (
                  <tr key={p.date}>
                    <td className="px-3 py-1">{p.date}</td>
                    <td className="px-3 py-1 text-right font-mono">{fmt(p.value, 2)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </div>
  );
}
