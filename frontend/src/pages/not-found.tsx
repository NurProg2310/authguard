import { Link } from "react-router-dom";
import { AuthLayout } from "@/components/auth-layout";
import { Button } from "@/components/ui/button";

export default function NotFoundPage() {
  return (
    <AuthLayout title="Page not found" description="The page you're looking for doesn't exist or was moved.">
      <Button asChild className="w-full">
        <Link to="/">Go home</Link>
      </Button>
    </AuthLayout>
  );
}
