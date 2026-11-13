import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Info } from "lucide-react";
import { toast } from "sonner";
import { AuthLayout } from "@/components/auth-layout";
import { FormField } from "@/components/form-field";
import { PasswordStrength } from "@/components/password-strength";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { ApiError, authApi } from "@/lib/api";
import { passwordRule } from "@/pages/register";

const schema = z
  .object({ password: passwordRule, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { path: ["confirm"], message: "Passwords don't match" });
type Values = z.infer<typeof schema>;

export default function ResetPasswordPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [token] = useState(() => params.get("token") ?? "");
  const [formError, setFormError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema), mode: "onTouched" });

  // Strip the token from the address bar so it doesn't linger in history.
  useEffect(() => {
    if (params.has("token")) window.history.replaceState(null, "", "/reset-password");
  }, [params]);

  const onSubmit = async (v: Values) => {
    setFormError(null);
    try {
      await authApi.resetPassword(token, v.password);
      toast.success("Password updated", { description: "Sign in with your new password." });
      navigate("/login", { replace: true });
    } catch (e) {
      setFormError(e instanceof ApiError ? e.message : "Something went wrong");
    }
  };

  if (!token) {
    return (
      <AuthLayout title="Invalid link" description="This password reset link is missing or malformed.">
        <Button asChild className="w-full">
          <Link to="/forgot-password">Request a new link</Link>
        </Button>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      title="Choose a new password"
      description="You'll be signed out of every device once it's updated."
      footer={
        <Link to="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
          Back to sign in
        </Link>
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} noValidate className="grid gap-5">
        {formError && (
          <Alert variant="destructive">
            <Info />
            <span>
              {formError}{" "}
              <Link to="/forgot-password" className="font-medium underline underline-offset-4">
                Request a new link
              </Link>
            </span>
          </Alert>
        )}
        <FormField
          label="New password"
          type="password"
          autoComplete="new-password"
          autoFocus
          error={errors.password?.message}
          hint={<PasswordStrength password={watch("password") ?? ""} />}
          {...register("password")}
        />
        <FormField
          label="Confirm password"
          type="password"
          autoComplete="new-password"
          error={errors.confirm?.message}
          {...register("confirm")}
        />
        <Button type="submit" size="lg" loading={isSubmitting} className="w-full">
          Update password
        </Button>
      </form>
    </AuthLayout>
  );
}
