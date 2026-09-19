// app/(site)/layout.tsx
import "@/styles/globals.css";
import { ReactNode } from "react";
import { Header } from "./components/Header";

export const metadata = {
  title: process.env.NEXT_PUBLIC_SITE_NAME || "Night of a Thousand Pixels",
  description: "2025 Spooky Movie Night Voting",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  const site =
    process.env.NEXT_PUBLIC_SITE_NAME || "Night of a Thousand Pixels";
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-black text-zinc-200">
        <div className="fixed inset-0 -z-10 bg-[radial-gradient(ellipse_at_top,_var(--tw-gradient-stops))] from-zinc-900 via-black to-black" />
        <Header title={site} />
        <main className="container mx-auto px-4 pb-24">{children}</main>
      </body>
    </html>
  );
}
