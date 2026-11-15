import { useCallback, useEffect, useState } from "react";
import { motion } from "framer-motion";
import { ArrowDownRight, ArrowUpRight, ExternalLink, Minus, RefreshCw } from "lucide-react";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { GoldChart } from "@/components/gold-chart";
import { goldApi, type GoldQuote, type GoldReport } from "@/lib/api";
import { cn, relativeTime } from "@/lib/utils";

const REFRESH_MS = 5 * 60_000;
const flags: Record<string, string> = { KZ: "🇰🇿", US: "🇺🇸", CN: "🇨🇳", RU: "🇷🇺", INTL: "🌐" };
const symbols: Record<string, string> = { KZT: "₸", USD: "$", CNY: "¥", RUB: "₽" };

const num = (v: number, digits = 2) =>
  new Intl.NumberFormat("ru-RU", { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(v);

export function GoldCard() {
  const [report, setReport] = useState<GoldReport | null>(null);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setReport(await goldApi.report());
      setError(false);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const i = window.setInterval(() => void load(), REFRESH_MS);
    return () => window.clearInterval(i);
  }, [load]);

  // Reference for the "vs NBK" column: the official Kazakhstan price.
  const nbk = report?.quotes.find((q) => q.id === "nbk" && q.available)?.kzt_per_gram;

  return (
    <Card className="overflow-hidden">
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div className="grid gap-1.5">
          <CardTitle className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full bg-gradient-to-br from-amber-300 to-amber-600 shadow-[0_0_8px_rgba(245,158,11,0.5)]" />
            Gold prices
          </CardTitle>
          <CardDescription>
            Exchanges worldwide and Kazakhstan, converted at the National Bank of Kazakhstan rate.
          </CardDescription>
        </div>
        <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" onClick={() => void load()} disabled={loading} title="Refresh">
          <RefreshCw className={loading ? "animate-spin" : ""} />
        </Button>
      </CardHeader>

      <GoldChart />

      {error && !report ? (
        <p className="px-6 pb-8 pt-2 text-center text-sm text-muted-foreground">Gold prices are temporarily unavailable.</p>
      ) : (
        <>
          <div className="overflow-x-auto border-t">
            <table className="w-full sm:min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/30 text-left text-xs text-muted-foreground">
                  <th className="py-2.5 pl-4 pr-2 font-medium sm:px-6">Market</th>
                  <th className="hidden px-3 py-2.5 text-right font-medium sm:table-cell">Price</th>
                  <th className="px-2 py-2.5 text-right font-medium sm:px-3">₸ / gram</th>
                  <th className="hidden px-3 py-2.5 text-right font-medium sm:table-cell">$ / oz</th>
                  <th className="hidden px-3 py-2.5 text-right font-medium sm:table-cell">vs NBK</th>
                  <th className="py-2.5 pl-2 pr-4 text-right font-medium sm:px-6">Change</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {report
                  ? report.quotes.map((q, i) => <Row key={q.id} q={q} nbk={nbk} index={i} />)
                  : Array.from({ length: 5 }).map((_, i) => (
                      <tr key={i}>
                        <td className="px-5 py-3.5 sm:px-6">
                          <Skeleton className="h-4 w-44" />
                        </td>
                        {Array.from({ length: 5 }).map((__, j) => (
                          <td key={j} className={cn("px-3 py-3.5", j !== 1 && j !== 4 && "hidden sm:table-cell")}>
                            <Skeleton className="ml-auto h-4 w-16" />
                          </td>
                        ))}
                      </tr>
                    ))}
              </tbody>
            </table>
          </div>

          {report && (
            <div className="flex flex-col gap-2 border-t bg-muted/30 px-5 py-3 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between sm:px-6">
              <div className="flex flex-wrap gap-x-4 gap-y-1">
                <span>NBK rates on {report.fx_date}:</span>
                {(["USD", "CNY", "RUB", "EUR"] as const)
                  .filter((c) => report.fx[c])
                  .map((c) => (
                    <span key={c} className="font-mono tabular-nums text-foreground/80">
                      {c} {num(report.fx[c])} ₸
                    </span>
                  ))}
              </div>
              <span>Updated {relativeTime(report.updated_at)} · prices may be delayed · not financial advice</span>
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function Row({ q, nbk, index }: { q: GoldQuote; nbk?: number; index: number }) {
  const diff = nbk && q.kzt_per_gram && q.id !== "nbk" ? ((q.kzt_per_gram - nbk) / nbk) * 100 : null;
  return (
    <motion.tr
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ delay: index * 0.04 }}
      className={cn("transition-colors hover:bg-muted/30", q.id === "nbk" && "bg-amber-500/[0.04]")}
    >
      <td className="py-3 pl-4 pr-2 sm:px-6">
        <a href={q.source_url} target="_blank" rel="noopener noreferrer" className="group flex items-center gap-3">
          <span className="text-lg leading-none" aria-hidden>
            {flags[q.country] ?? "🏳️"}
          </span>
          <span className="min-w-0">
            <span className="flex items-center gap-1 font-medium">
              {q.name}
              <ExternalLink className="h-3 w-3 opacity-0 transition-opacity group-hover:opacity-50" />
            </span>
            <span className="block text-xs text-muted-foreground">{q.venue}</span>
          </span>
        </a>
      </td>
      {q.available ? (
        <>
          <td className="hidden px-3 py-3 text-right font-mono tabular-nums sm:table-cell">
            {symbols[q.currency] ?? q.currency}
            {num(q.price ?? 0)}
            <span className="ml-1 text-xs text-muted-foreground">/{q.unit}</span>
          </td>
          <td className="whitespace-nowrap px-2 py-3 text-right font-mono font-medium tabular-nums sm:px-3">
            {q.kzt_per_gram ? num(q.kzt_per_gram) : "—"}
          </td>
          <td className="hidden px-3 py-3 text-right font-mono tabular-nums text-muted-foreground sm:table-cell">
            {q.usd_per_oz ? num(q.usd_per_oz) : "—"}
          </td>
          <td className="hidden px-3 py-3 text-right font-mono text-xs tabular-nums text-muted-foreground sm:table-cell">
            {diff === null ? (q.id === "nbk" ? "base" : "—") : `${diff > 0 ? "+" : ""}${num(diff)}%`}
          </td>
          <td className="py-3 pl-2 pr-4 text-right sm:px-6">
            <Change value={q.change_pct} />
          </td>
        </>
      ) : (
        <td colSpan={5} className="px-5 py-3 text-right text-xs text-muted-foreground sm:px-6">
          Temporarily unavailable
        </td>
      )}
    </motion.tr>
  );
}

function Change({ value }: { value?: number }) {
  if (value === undefined || value === null) return <span className="text-xs text-muted-foreground">—</span>;
  const up = value > 0;
  const flat = value === 0;
  const Icon = flat ? Minus : up ? ArrowUpRight : ArrowDownRight;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-0.5 rounded-md px-1.5 py-0.5 font-mono text-xs tabular-nums",
        flat ? "text-muted-foreground" : up ? "bg-success/10 text-success" : "bg-destructive/10 text-destructive",
      )}
    >
      <Icon className="h-3 w-3" />
      {up ? "+" : ""}
      {num(value)}%
    </span>
  );
}
