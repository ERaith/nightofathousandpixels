-- AlterTable
ALTER TABLE "Movie" ADD COLUMN "deviceId" TEXT;

-- CreateTable
CREATE TABLE "VoterWhitelist" (
    "id" TEXT NOT NULL PRIMARY KEY,
    "seasonId" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "name" TEXT,
    "hasVoted" BOOLEAN NOT NULL DEFAULT false,
    "votedAt" DATETIME,
    "createdAt" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "VoterWhitelist_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);

-- CreateIndex
CREATE UNIQUE INDEX "VoterWhitelist_seasonId_email_key" ON "VoterWhitelist"("seasonId", "email");
