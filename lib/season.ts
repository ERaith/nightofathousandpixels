// lib/season.ts
import { prisma } from "./db";

export async function getOrCreateCurrentSeason() {
  const y = new Date().getFullYear();
  let s = await prisma.season.findUnique({ where: { year: y } });
  if (!s) {
    s = await prisma.season.create({
      data: { year: y, name: String(y) },
    });
  }
  return s;
}

export function isLocked(season: {
  lockAt: Date | string | null;
  locked: boolean;
}) {
  if (season.locked) return true;
  if (!season.lockAt) return false;
  const lockDate =
    typeof season.lockAt === "string" ? new Date(season.lockAt) : season.lockAt;
  return lockDate.getTime() <= Date.now();
}
