import { useState } from "react";
import { Link } from "react-router-dom";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowLeft, Info, MailCheck } from "lucide-react";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { ApiError, authApi } from "@/lib/api";

const schema = z.object({
  email: z.string().trim().min(1, "Email is required").email("Enter a valid email address"),
});
type Values = z.infer<typeof schema>;

export default function ForgotPasswordPage() {
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema) });

  const onSubmit = async (v: Values) => {
    setFormError(null);
    try {
      await authApi.forgotPassword(v.email);
      setSentTo(v.email);
    } catch (e) {
      setFormError(e instanceof ApiError ? e.message : "Something went wrong");
    }
  };

  return (
    <AuthLayout
      title={sentTo ? "Check your inbox" : "Reset your password"}
      description={
        sentTo ? undefined : "Enter the email you signed up with and we'll send you a secure reset link."
      }
      footer={
        <Link
          to="/login"
          className="inline-flex items-center gap-1.5 font-medium text-foreground underline-offset-4 hover:underline"
        >
          <ArrowLeft className="h-3.5 w-3.5" /> Back to sign in
        </Link>
      }
    >
      <AnimatePresence mode="wait" initial={false}>
        {sentTo ? (
          <motion.div
            key="sent"
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            className="grid justify-items-center gap-4 py-2 text-center"
          >
            <div className="flex h-12 w-12 items-center justify-center rounded-full border bg-muted/60">
              <MailCheck className="h-5 w-5" />
            </div>
            <p className="text-sm text-muted-foreground">
              If an account exists for <span className="font-medium text-foreground">{sentTo}</span>, you'll receive
              a reset link shortly. It expires in 30 minutes.
            </p>
            <Button variant="outline" className="w-full" onClick={() => setSentTo(null)}>
              Use a different email
            </Button>
          </motion.div>
        ) : (
          <motion.form
            key="form"
            exit={{ opacity: 0, scale: 0.98 }}
            onSubmit={handleSubmit(onSubmit)}
            noValidate
            className="grid gap-5"
          >
            {formError && (
              <Alert variant="destructive">
                <Info />
                <span>{formError}</span>
              </Alert>
            )}
            <FormField
              label="Email"
              type="email"
              autoComplete="email"
              autoFocus
              placeholder="you@company.com"
              error={errors.email?.message}
              {...register("email")}
            />
            <Button type="submit" size="lg" loading={isSubmitting} className="w-full">
              Send reset link
            </Button>
          </motion.form>
        )}
      </AnimatePresence>
    </AuthLayout>
  );
}
