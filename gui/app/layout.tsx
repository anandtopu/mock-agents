import "./globals.css";
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import { getBaseUrl, getHealth } from "@/lib/api";
import { AuthStatus, getAuthStatus, logout } from "@/lib/auth";

export const metadata: Metadata = {
  title: "MockAgents",
  description: "Browse agent catalog, inspect definitions, and view interaction logs.",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Health check runs on every navigation — cheap against the local
  // mock server and lets us surface connectivity issues immediately.
  const health = await getHealth();
  const auth = await getAuthStatus();

  async function logoutAction() {
    "use server";
    await logout();
    redirect("/login");
  }

  return (
    <html lang="en">
      <body>
        <header className="header">
          <div className="brand">
            <Link href="/">MockAgents</Link>
            <span className="subtitle">Web console · v0.3</span>
          </div>
          <nav className="nav">
            <Link href="/">Agents</Link>
            <Link href="/pipelines">Pipelines</Link>
            <Link href="/logs">Logs</Link>
            <Link href="/costs">Costs</Link>
            <Link href="/audit">Audit</Link>
            <Link href="/editor">Editor</Link>
            <Link href="/admin/tenants">Admin</Link>
            <a href={getBaseUrl()} target="_blank" rel="noreferrer" className="muted">
              API →
            </a>
          </nav>
          <div className="header-right">
            <HealthPill health={health} apiUrl={getBaseUrl()} />
            <AuthPill auth={auth} logoutAction={logoutAction} />
          </div>
        </header>

        <main className="main">{children}</main>

        <footer className="footer">
          <span>MockAgents · talking to {getBaseUrl()}</span>
        </footer>
      </body>
    </html>
  );
}

function AuthPill({
  auth,
  logoutAction,
}: {
  auth: AuthStatus | null;
  logoutAction: () => Promise<void>;
}) {
  if (!auth) {
    return (
      <Link href="/login" className="pill pill-muted">
        <span className="dot" /> sign in
      </Link>
    );
  }
  return (
    <form action={logoutAction} className="auth-pill">
      <Link href="/account" className="pill pill-ok" title={`Signed in · role ${auth.role} — click for self-rotation`}>
        <span className="dot" /> {auth.prefix}…
      </Link>
      <button type="submit" className="btn btn-xsmall">
        Sign out
      </button>
    </form>
  );
}

function HealthPill({
  health,
  apiUrl,
}: {
  health: { status: string; version?: string } | null;
  apiUrl: string;
}) {
  if (!health) {
    return (
      <div className="pill pill-down" title={`unreachable: ${apiUrl}`}>
        <span className="dot" /> offline
      </div>
    );
  }
  return (
    <div className="pill pill-ok" title={`version ${health.version ?? "unknown"}`}>
      <span className="dot" /> online
    </div>
  );
}
