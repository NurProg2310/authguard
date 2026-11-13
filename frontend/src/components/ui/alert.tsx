import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const alertVariants = cva(
  "relative flex w-full gap-3 rounded-lg border px-3.5 py-3 text-sm [&>svg]:mt-0.5 [&>svg]:size-4 [&>svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-muted/50 text-foreground [&>svg]:text-muted-foreground",
        destructive: "border-destructive/30 bg-destructive/[0.06] text-destructive [&>svg]:text-destructive",
        warning: "border-warning/30 bg-warning/[0.07] text-foreground [&>svg]:text-warning",
        success: "border-success/30 bg-success/[0.07] text-foreground [&>svg]:text-success",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export function Alert({
  className,
  variant,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>) {
  return <div role="alert" className={cn(alertVariants({ variant }), className)} {...props} />;
}
