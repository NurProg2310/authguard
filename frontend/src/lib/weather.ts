import {
  Cloud,
  CloudDrizzle,
  CloudFog,
  CloudLightning,
  CloudMoon,
  CloudRain,
  CloudSnow,
  CloudSun,
  Moon,
  Snowflake,
  Sun,
  type LucideIcon,
} from "lucide-react";

/** Maps WMO weather codes (used by Open-Meteo) to a label and icon. */
export function describeWeather(code: number, isDay = true): { label: string; icon: LucideIcon } {
  if (code === 0) return { label: "Clear", icon: isDay ? Sun : Moon };
  if (code <= 2) return { label: code === 1 ? "Mostly clear" : "Partly cloudy", icon: isDay ? CloudSun : CloudMoon };
  if (code === 3) return { label: "Overcast", icon: Cloud };
  if (code === 45 || code === 48) return { label: "Fog", icon: CloudFog };
  if (code >= 51 && code <= 57) return { label: "Drizzle", icon: CloudDrizzle };
  if ((code >= 61 && code <= 67) || (code >= 80 && code <= 82)) return { label: code >= 80 ? "Showers" : "Rain", icon: CloudRain };
  if (code >= 71 && code <= 77) return { label: "Snow", icon: Snowflake };
  if (code === 85 || code === 86) return { label: "Snow showers", icon: CloudSnow };
  if (code >= 95) return { label: "Thunderstorm", icon: CloudLightning };
  return { label: "Unknown", icon: Cloud };
}

export interface SavedLocation {
  latitude: number;
  longitude: number;
  name: string | null; // null = from geolocation; derive from timezone
}

const KEY = "authguard-weather-location";

export function loadLocation(): SavedLocation | null {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "null");
    if (v && typeof v.latitude === "number" && typeof v.longitude === "number") return v;
  } catch {
    /* ignore */
  }
  return null;
}

export function saveLocation(loc: SavedLocation | null) {
  try {
    if (loc) localStorage.setItem(KEY, JSON.stringify(loc));
    else localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}

/** "Asia/Almaty" -> "Almaty" — a decent label when we only have coordinates. */
export function cityFromTimezone(tz: string): string {
  const last = tz.split("/").pop() ?? "";
  return last.replace(/_/g, " ") || "Your location";
}

export function getBrowserPosition(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) => {
    if (!("geolocation" in navigator)) return reject(new Error("unsupported"));
    navigator.geolocation.getCurrentPosition(resolve, reject, { timeout: 8000, maximumAge: 30 * 60_000 });
  });
}

export const round = (n: number) => Math.round(n);
