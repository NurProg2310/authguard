export type DeviceKind = "desktop" | "mobile" | "tablet";

export interface ParsedUA {
  browser: string;
  os: string;
  kind: DeviceKind;
  label: string;
}

/** Small, dependency-free user-agent summariser for display purposes only. */
export function parseUA(ua: string): ParsedUA {
  const s = ua || "";
  const browser =
    match(s, /Edg(?:e|A|iOS)?\/([\d]+)/, "Edge") ??
    match(s, /OPR\/([\d]+)/, "Opera") ??
    match(s, /Firefox\/([\d]+)/, "Firefox") ??
    match(s, /FxiOS\/([\d]+)/, "Firefox") ??
    match(s, /CriOS\/([\d]+)/, "Chrome") ??
    match(s, /Chrome\/([\d]+)/, "Chrome") ??
    match(s, /Version\/([\d]+).*Safari/, "Safari") ??
    (/curl\//i.test(s) ? "curl" : "Unknown browser");

  let os = "Unknown OS";
  if (/iPad/.test(s)) os = "iPadOS";
  else if (/iPhone|iPod/.test(s)) os = "iOS";
  else if (/Android/.test(s)) os = "Android";
  else if (/Windows NT/.test(s)) os = "Windows";
  else if (/Mac OS X|Macintosh/.test(s)) os = "macOS";
  else if (/CrOS/.test(s)) os = "ChromeOS";
  else if (/Linux/.test(s)) os = "Linux";

  const kind: DeviceKind = /iPad|Tablet/.test(s) ? "tablet" : /Mobi|iPhone|Android/.test(s) ? "mobile" : "desktop";
  return { browser, os, kind, label: `${browser} on ${os}` };
}

function match(s: string, re: RegExp, name: string): string | null {
  const m = s.match(re);
  return m ? `${name} ${m[1]}` : null;
}
