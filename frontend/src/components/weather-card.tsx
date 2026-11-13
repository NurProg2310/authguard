import { useCallback, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Droplets, LocateFixed, MapPin, Search, Thermometer, Wind, X } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { weatherApi, type Place, type WeatherForecast } from "@/lib/api";
import {
  cityFromTimezone,
  describeWeather,
  getBrowserPosition,
  loadLocation,
  round,
  saveLocation,
  type SavedLocation,
} from "@/lib/weather";
import { cn } from "@/lib/utils";

type State =
  | { kind: "loading" }
  | { kind: "ready"; forecast: WeatherForecast; name: string }
  | { kind: "needs-location"; message: string }
  | { kind: "error"; message: string };

const lang = (navigator.language || "en").slice(0, 2).toLowerCase();
const weekday = new Intl.DateTimeFormat(undefined, { weekday: "short" });

export function WeatherCard() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [searching, setSearching] = useState(false);

  const show = useCallback(async (loc: SavedLocation) => {
    setState({ kind: "loading" });
    try {
      const { forecast } = await weatherApi.forecast(loc.latitude, loc.longitude);
      setState({ kind: "ready", forecast, name: loc.name ?? cityFromTimezone(forecast.timezone) });
    } catch {
      setState({ kind: "error", message: "Weather is temporarily unavailable." });
    }
  }, []);

  const locate = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      const pos = await getBrowserPosition();
      const loc = { latitude: pos.coords.latitude, longitude: pos.coords.longitude, name: null };
      saveLocation(loc);
      await show(loc);
    } catch {
      setState({ kind: "needs-location", message: "Location access is off. Search for a city instead." });
      setSearching(true);
    }
  }, [show]);

  useEffect(() => {
    const saved = loadLocation();
    if (saved) void show(saved);
    else void locate();
  }, [show, locate]);

  const pick = (p: Place) => {
    const loc = { latitude: p.latitude, longitude: p.longitude, name: p.name };
    saveLocation(loc);
    setSearching(false);
    void show(loc);
  };

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between gap-3 border-b px-5 py-3 sm:px-6">
        <div className="flex min-w-0 items-center gap-2 text-sm">
          <MapPin className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate font-medium">
            {state.kind === "ready" ? state.name : "Weather"}
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => void locate()} title="Use my location">
            <LocateFixed />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={() => setSearching((v) => !v)}
            title={searching ? "Close search" : "Change city"}
          >
            {searching ? <X /> : <Search />}
          </Button>
        </div>
      </div>

      <AnimatePresence initial={false}>
        {searching && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="overflow-hidden border-b"
          >
            <CitySearch onPick={pick} />
          </motion.div>
        )}
      </AnimatePresence>

      <div className="p-5 sm:p-6">
        {state.kind === "loading" && <WeatherSkeleton />}
        {(state.kind === "error" || state.kind === "needs-location") && (
          <p className="py-6 text-center text-sm text-muted-foreground">{state.message}</p>
        )}
        {state.kind === "ready" && <WeatherBody forecast={state.forecast} />}
      </div>
    </Card>
  );
}

function WeatherBody({ forecast }: { forecast: WeatherForecast }) {
  const c = forecast.current;
  const { label, icon: Icon } = describeWeather(c.weather_code, c.is_day);
  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, ease: [0.16, 1, 0.3, 1] }}
      className="grid gap-6 md:grid-cols-[auto_1fr] md:items-center md:gap-10"
    >
      <div className="flex items-center gap-4">
        <div className="flex h-16 w-16 items-center justify-center rounded-2xl border bg-gradient-to-b from-background to-muted/60 shadow-sm">
          <Icon className="h-8 w-8" strokeWidth={1.5} />
        </div>
        <div>
          <div className="flex items-start">
            <span className="text-5xl font-semibold tracking-[-0.04em] tabular-nums">{round(c.temperature)}</span>
            <span className="mt-1.5 text-lg text-muted-foreground">°C</span>
          </div>
          <p className="text-sm text-muted-foreground">{label}</p>
        </div>
        <dl className="ml-2 hidden gap-1.5 text-xs text-muted-foreground sm:grid">
          <Metric icon={<Thermometer />} label="Feels like" value={`${round(c.apparent_temperature)}°`} />
          <Metric icon={<Droplets />} label="Humidity" value={`${round(c.humidity)}%`} />
          <Metric icon={<Wind />} label="Wind" value={`${round(c.wind_speed)} km/h`} />
        </dl>
      </div>

      <ol className="grid grid-cols-5 gap-1.5">
        {forecast.daily.map((d, i) => {
          const w = describeWeather(d.weather_code);
          return (
            <li
              key={d.date}
              className={cn(
                "flex flex-col items-center gap-1.5 rounded-lg border border-transparent px-1 py-2.5 text-center",
                i === 0 && "border-border bg-muted/40",
              )}
              title={w.label}
            >
              <span className="text-[11px] font-medium text-muted-foreground">
                {i === 0 ? "Today" : weekday.format(new Date(d.date + "T12:00:00"))}
              </span>
              <w.icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
              <span className="text-xs tabular-nums">
                {round(d.temp_max)}°<span className="ml-1 text-muted-foreground">{round(d.temp_min)}°</span>
              </span>
            </li>
          );
        })}
      </ol>
    </motion.div>
  );
}

function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="flex items-center gap-1.5 [&_svg]:h-3 [&_svg]:w-3">
      {icon}
      <dt>{label}</dt>
      <dd className="font-medium text-foreground tabular-nums">{value}</dd>
    </div>
  );
}

function CitySearch({ onPick }: { onPick: (p: Place) => void }) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<Place[]>([]);
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) {
      setResults([]);
      return;
    }
    const id = ++seq.current;
    setLoading(true);
    const t = window.setTimeout(() => {
      weatherApi
        .search(term, lang)
        .then((r) => id === seq.current && setResults(r.places))
        .catch(() => id === seq.current && setResults([]))
        .finally(() => id === seq.current && setLoading(false));
    }, 300);
    return () => window.clearTimeout(t);
  }, [q]);

  return (
    <div className="p-3">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search city…"
          className="h-9 pl-8"
          aria-label="Search city"
        />
      </div>
      {(results.length > 0 || (loading && q.trim().length >= 2)) && (
        <ul className="mt-2 max-h-56 overflow-auto">
          {loading && results.length === 0 && <li className="px-2 py-2 text-xs text-muted-foreground">Searching…</li>}
          {results.map((p) => (
            <li key={`${p.latitude},${p.longitude}`}>
              <button
                type="button"
                onClick={() => onPick(p)}
                className="flex w-full items-baseline gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
              >
                <span className="font-medium">{p.name}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {[p.region, p.country].filter(Boolean).join(", ")}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {!loading && q.trim().length >= 2 && results.length === 0 && (
        <p className="mt-2 px-2 text-xs text-muted-foreground">No cities found.</p>
      )}
    </div>
  );
}

function WeatherSkeleton() {
  return (
    <div className="grid gap-6 md:grid-cols-[auto_1fr] md:items-center md:gap-10">
      <div className="flex items-center gap-4">
        <Skeleton className="h-16 w-16 rounded-2xl" />
        <div className="grid gap-2">
          <Skeleton className="h-10 w-20" />
          <Skeleton className="h-3.5 w-24" />
        </div>
      </div>
      <div className="grid grid-cols-5 gap-1.5">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-[76px] rounded-lg" />
        ))}
      </div>
    </div>
  );
}
