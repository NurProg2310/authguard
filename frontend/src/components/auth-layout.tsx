import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { motion } from "framer-motion";
import { Logo } from "@/components/logo";
import { ThemeToggle } from "@/components/theme-toggle";

interface Props {
  title: string;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}

export function AuthLayout({ title, description, children, footer }: Props) {
  return (
    <div className="relative flex min-h-dvh flex-col overflow-hidden bg-background">
      {/* Backdrop: fading grid + soft top glow */}
      <div aria-hidden className="pointer-events-none absolute inset-0 bg-grid mask-fade-top" />
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-0 h-[420px] w-[min(900px,140vw)] -translate-x-1/2 -translate-y-1/2 rounded-full opacity-60 blur-3xl"
        style={{ background: "radial-gradient(closest-side, hsl(var(--glow) / 0.35), transparent)" }}
      />

      <header className="relative z-10 flex items-center justify-between px-4 py-4 sm:px-8 sm:py-5">
        <Link to="/" className="rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <Logo />
        </Link>
        <ThemeToggle />
      </header>

      <main className="relative z-10 flex flex-1 items-center justify-center px-4 pb-16 pt-6">
        <motion.div
          initial={{ opacity: 0, y: 12, filter: "blur(4px)" }}
          animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
          transition={{ duration: 0.5, ease: [0.16, 1, 0.3, 1] }}
          className="w-full max-w-[400px]"
        >
          <div className="mb-7 text-center">
            <h1 className="text-balance text-[26px] font-semibold tracking-[-0.03em]">{title}</h1>
            {description && <p className="mt-2 text-balance text-sm text-muted-foreground">{description}</p>}
          </div>
          <div className="rounded-xl border bg-card/80 p-6 shadow-[0_1px_2px_rgba(0,0,0,0.04),0_8px_24px_-12px_rgba(0,0,0,0.12)] backdrop-blur-sm sm:p-7 dark:shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_8px_32px_-12px_rgba(0,0,0,0.6)]">
            {children}
          </div>
          {footer && <div className="mt-6 text-center text-sm text-muted-foreground">{footer}</div>}
        </motion.div>
      </main>

      <footer className="relative z-10 px-4 pb-6 text-center text-xs text-muted-foreground/70">
        Protected by single-session enforcement · Argon2id · rotating refresh tokens
      </footer>
    </div>
  );
}
