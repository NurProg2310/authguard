import { forwardRef, useId, useState, type InputHTMLAttributes, type ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Eye, EyeOff } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface FieldProps extends InputHTMLAttributes<HTMLInputElement> {
  label: string;
  error?: string;
  hint?: ReactNode;
  labelAction?: ReactNode;
}

export const FormField = forwardRef<HTMLInputElement, FieldProps>(
  ({ label, error, hint, labelAction, id, type, className, ...props }, ref) => {
    const autoId = useId();
    const inputId = id ?? autoId;
    const errId = `${inputId}-error`;
    const [reveal, setReveal] = useState(false);
    const isPassword = type === "password";

    return (
      <div className={cn("grid gap-2", className)}>
        <div className="flex items-center justify-between">
          <Label htmlFor={inputId}>{label}</Label>
          {labelAction}
        </div>
        <div className="relative">
          <Input
            ref={ref}
            id={inputId}
            type={isPassword && reveal ? "text" : type}
            aria-invalid={!!error}
            aria-describedby={error ? errId : undefined}
            className={cn(isPassword && "pr-10")}
            {...props}
          />
          {isPassword && (
            <button
              type="button"
              onClick={() => setReveal((v) => !v)}
              className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-md text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              aria-label={reveal ? "Hide password" : "Show password"}
              tabIndex={-1}
            >
              {reveal ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          )}
        </div>
        <AnimatePresence initial={false} mode="wait">
          {error ? (
            <motion.p
              key="err"
              id={errId}
              initial={{ opacity: 0, height: 0, y: -2 }}
              animate={{ opacity: 1, height: "auto", y: 0 }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.18 }}
              className="text-xs text-destructive"
            >
              {error}
            </motion.p>
          ) : hint ? (
            <motion.div key="hint" initial={false} className="text-xs text-muted-foreground">
              {hint}
            </motion.div>
          ) : null}
        </AnimatePresence>
      </div>
    );
  },
);
FormField.displayName = "FormField";
