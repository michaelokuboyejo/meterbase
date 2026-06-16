import { LoginForm } from "./login-form";

export default function LoginPage() {
  return (
    <div className="bg-card border-border w-full max-w-sm rounded-lg border p-8 shadow-sm">
      <div className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">MeterBase</h1>
        <p className="text-muted-foreground mt-1 text-sm">Sign in to your dashboard</p>
      </div>
      <LoginForm />
    </div>
  );
}
