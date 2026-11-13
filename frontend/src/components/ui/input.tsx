import * as React from "react";
import { cn } from "@/lib/utils";

const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => (
    <input
      type={type}
      ref={ref}
      className={cn(
        "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-[border-color,box-shadow] duration-150",
        "placeholder:text-muted-foreground/70 file:border-0 file:bg-transparent file:text-sm file:font-medium",
        "hover:border-foreground/20 focus-visible:border-foreground/40 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-ring/10",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "aria-[invalid=true]:border-destructive/70 aria-[invalid=true]:focus-visible:ring-destructive/15",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export { Input };
