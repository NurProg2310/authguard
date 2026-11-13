import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AnimatePresence, motion } from "framer-motion";
import { Info, ArrowRight } from "lucide-react";
import { toast } from "sonner";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { SessionConflictDialog } from "@/components/session-conflict-dialog";
import { useAuth } from "@/lib/auth";
import { ApiError, conflictFrom, type SessionConflict } from "@/lib/api";

const schema = z.object({
  email: z.string().trim().min(1, "Email is required").email("Enter a valid email address"),
  password: z.string().min(1, "Password is required"),
});
type Values = z.infer<typeof schema>;

export default function LoginPage() {
  const { login, takeover, notice, clearNotice } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/dashboard";

  const [formError, setFormError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<SessionConflict | null>(null);
  const [takingOver, setTakingOver] = useState(false);

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema) });

  const onSubmit = async (v: Values) => {
    setFormError(null);
    clearNotice();
    try {
      await login(v.email, v.password);
      navigate(from, { replace: true });
    } catch (e) {
      const c = conflictFrom(e);
      if (c) return setConflict(c);
      setFormError(e instanceof ApiError ? e.message : "Something went wrong");
    }
  };

  const onTakeover = async () => {
    if (!conflict) return;
    setTakingOver(true);
    try {
      await takeover(conflict.takeover_token);
      setConflict(null);
      toast.success("Signed in", { description: "Your other session was signed out." });
      navigate(from, { replace: true });
    } catch (e) {
      setConflict(null);
      setFormError(e instanceof ApiError ? e.message : "Couldn't complete sign-in");
    } finally {
      setTakingOver(false);
    }
  };

  return (
    <AuthLayout
      title="Welcome back"
      description="Sign in to your AuthGuard account"
      footer={
        <>
          Don't have an account?{" "}
          <Link to="/register" className="font-medium text-foreground underline-offset-4 hover:underline">
            Create one
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} noValidate className="grid gap-5">
        <AnimatePresence initial={false}>
          {(notice || formError) && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.2 }}
            >
              <Alert variant={formError ? "destructive" : "warning"}>
                <Info />
                <span>{formError ?? notice}</span>
              </Alert>
            </motion.div>
          )}
        </AnimatePresence>

        <FormField
          label="Email"
          type="email"
          autoComplete="email"
          autoFocus
          placeholder="you@company.com"
          error={errors.email?.message}
          {...register("email")}
        />
        <FormField
          label="Password"
          type="password"
          autoComplete="current-password"
          placeholder="••••••••"
          error={errors.password?.message}
          labelAction={
            <Link
              to="/forgot-password"
              className="text-xs text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
            >
              Forgot password?
            </Link>
          }
          {...register("password")}
        />

        <Button type="submit" size="lg" loading={isSubmitting} className="group mt-1 w-full">
          Sign in
          {!isSubmitting && <ArrowRight className="transition-transform group-hover:translate-x-0.5" />}
        </Button>
      </form>

      <SessionConflictDialog
        conflict={conflict}
        pending={takingOver}
        onCancel={() => setConflict(null)}
        onConfirm={onTakeover}
      />
    </AuthLayout>
  );
}
