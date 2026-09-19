// app/(site)/admin/page.tsx
import { getAdminSession } from "@/lib/auth";
import { redirect } from "next/navigation";
import AdminPanel from "./AdminPanel";

export const dynamic = 'force-dynamic';

export default async function AdminPage() {
  const session = await getAdminSession();

  if (!session) {
    redirect("/admin/login");
  }

  return <AdminPanel />;
}
