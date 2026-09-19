-- AlterTable
ALTER TABLE "Vote" ADD COLUMN "email" TEXT;

-- CreateIndex
CREATE UNIQUE INDEX "Vote_seasonId_email_key" ON "Vote"("seasonId", "email");
