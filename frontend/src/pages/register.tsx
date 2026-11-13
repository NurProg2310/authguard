import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
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
import { useAuth } from "@/lib/auth";
import { ApiError } from "@/lib/api";

export const passwordRule = z
  .string()
  .min(8, "Use at least 8 characters")
  .max(128, "Use at most 128 characters")
  .regex(/\p{L}/u, "Include at least one letter")
  .regex(/\d/, "Include at least one number");

const schema = z.object({
  name: z.string().trim().min(1, "Name is required").max(100, "Name is too long"),
  email: z.string().trim().min(1, "Email is required").email("Enter a valid email address"),
  password: passwordRule,
});
type Values = z.infer<typeof schema>;

export default function RegisterPage() {
  const { register: signUp } = useAuth();
  const navigate = useNavigate();
  const [formError, setFormError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    watch,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema), mode: "onTouched" });
  const password = watch("password") ?? "";

  const onSubmit = async (v: Values) => {
    setFormError(null);
    try {
      await signUp(v.name, v.email, v.password);
      toast.success("Account created", { description: "Welcome to AuthGuard." });
      navigate("/dashboard", { replace: true });
    } catch (e) {
      if (e instanceof ApiError && Object.keys(e.fields).length) {
        for (const [k, msg] of Object.entries(e.fields)) setError(k as keyof Values, { message: msg });
        return;
      }
      setFormError(e instanceof ApiError ? e.message : "Something went wrong");
    }
  };

  return (
    <AuthLayout
      title="Create your account"
      description="Secure by default. One device, one session."
      footer={
        <>
          Already have an account?{" "}
          <Link to="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} noValidate className="grid gap-5">
        {formError && (
          <Alert variant="destructive">
            <Info />
            <span>{formError}</span>
          </Alert>
        )}
        <FormField
          label="Name"
          autoComplete="name"
          autoFocus
          placeholder="Ada Lovelace"
          error={errors.name?.message}
          {...register("name")}
        />
        <FormField
          label="Email"
          type="email"
          autoComplete="email"
          placeholder="you@company.com"
          error={errors.email?.message}
          {...register("email")}
        />
        <FormField
          label="Password"
          type="password"
          autoComplete="new-password"
          placeholder="At least 8 characters"
          error={errors.password?.message}
          hint={<PasswordStrength password={password} />}
          {...register("password")}
        />
        <Button type="submit" size="lg" loading={isSubmitting} className="mt-1 w-full">
          Create account
        </Button>
        <p className="text-center text-xs text-muted-foreground">
          Passwords are hashed with Argon2id and never stored in plain text.
        </p>
      </form>
    </AuthLayout>
  );
}
