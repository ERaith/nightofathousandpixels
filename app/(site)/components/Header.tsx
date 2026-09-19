// app/(site)/components/Header.tsx
"use client";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

function HeaderContent({ title }: { title: string }) {
  const searchParams = useSearchParams();
  const hasAdminToken = searchParams.get("admin") !== null;

  return (
    <header className="sticky top-0 z-20 backdrop-blur border-b border-zinc-800 bg-black/60">
      <div className="container mx-auto px-4 py-4 flex items-center justify-between gap-4">
        <h1 className="text-lg md:text-2xl font-semibold tracking-wider flex items-center gap-2">
          <img src="/wy-logo.svg" alt="WY" className="h-5 w-5 opacity-70" />
          <span className="text-accent">⟁</span>
          <span className="hidden sm:inline">{title}</span>
        </h1>
        <nav className="flex items-center gap-3 md:gap-4 text-sm">
          <a className="hover:text-accent transition-colors px-2" href="/">
            Vote
          </a>
          <a className="hover:text-accent transition-colors px-2" href="/submit">
            Submit
          </a>
          <a className="hover:text-accent transition-colors px-2" href="/my-submissions">
            My Submissions
          </a>
          {hasAdminToken && (
            <a className="hover:text-accent transition-colors px-2 opacity-50" href="/admin">
              Admin
            </a>
          )}
        </nav>
      </div>
    </header>
  );
}

export function Header({ title }: { title: string }) {
  return (
    <Suspense fallback={
      <header className="sticky top-0 z-20 backdrop-blur border-b border-zinc-800 bg-black/60">
        <div className="container mx-auto px-4 py-4 flex items-center justify-between gap-4">
          <h1 className="text-lg md:text-2xl font-semibold tracking-wider flex items-center gap-2">
            <img src="/wy-logo.svg" alt="WY" className="h-5 w-5 opacity-70" />
            <span className="text-accent">⟁</span>
            <span className="hidden sm:inline">{title}</span>
          </h1>
          <nav className="flex items-center gap-3 md:gap-4 text-sm">
            <a className="hover:text-accent transition-colors px-2" href="/">Vote</a>
            <a className="hover:text-accent transition-colors px-2" href="/submit">Submit</a>
          </nav>
        </div>
      </header>
    }>
      <HeaderContent title={title} />
    </Suspense>
  );
}
