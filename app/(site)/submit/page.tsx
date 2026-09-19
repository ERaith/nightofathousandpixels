// app/(site)/submit/page.tsx
import SubmitForm from "../components/SubmitForm";
import SeasonBadge from "../components/SeasonBadge";
import { getOrCreateCurrentSeason } from "@/lib/season";

export const dynamic = 'force-dynamic';

export default async function SubmitPage() {
  const season = await getOrCreateCurrentSeason();

  return (
    <div className="max-w-2xl mx-auto mt-8 mb-16">
      <SeasonBadge season={season} />
      <SubmitForm season={season} />
    </div>
  );
}
