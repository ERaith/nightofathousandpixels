-- CreateTable
CREATE TABLE "Season" (
    "id" TEXT NOT NULL,
    "year" INTEGER NOT NULL,
    "name" TEXT NOT NULL DEFAULT '',
    "lockAt" TIMESTAMP(3),
    "locked" BOOLEAN NOT NULL DEFAULT false,
    "defaultSubmitLimit" INTEGER NOT NULL DEFAULT 1,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Season_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Movie" (
    "id" TEXT NOT NULL,
    "seasonId" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "description" TEXT,
    "trailerUrl" TEXT,
    "submittedBy" TEXT,
    "deviceId" TEXT,
    "submittedAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "hidden" BOOLEAN NOT NULL DEFAULT false,

    CONSTRAINT "Movie_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "Vote" (
    "id" TEXT NOT NULL,
    "seasonId" TEXT NOT NULL,
    "movieId" TEXT NOT NULL,
    "deviceId" TEXT NOT NULL,
    "email" TEXT,
    "ipHash" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "Vote_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "SubmissionOverride" (
    "id" TEXT NOT NULL,
    "seasonId" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "limit" INTEGER NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,

    CONSTRAINT "SubmissionOverride_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "VoterWhitelist" (
    "id" TEXT NOT NULL,
    "seasonId" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "name" TEXT,
    "hasVoted" BOOLEAN NOT NULL DEFAULT false,
    "votedAt" TIMESTAMP(3),
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "VoterWhitelist_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "AdminUser" (
    "id" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "passwordHash" TEXT NOT NULL,
    "name" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "lastLoginAt" TIMESTAMP(3),

    CONSTRAINT "AdminUser_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "Season_year_key" ON "Season"("year");

-- CreateIndex
CREATE UNIQUE INDEX "Movie_seasonId_title_key" ON "Movie"("seasonId", "title");

-- CreateIndex
CREATE INDEX "Vote_movieId_idx" ON "Vote"("movieId");

-- CreateIndex
CREATE UNIQUE INDEX "Vote_seasonId_deviceId_key" ON "Vote"("seasonId", "deviceId");

-- CreateIndex
CREATE UNIQUE INDEX "Vote_seasonId_email_key" ON "Vote"("seasonId", "email");

-- CreateIndex
CREATE UNIQUE INDEX "SubmissionOverride_seasonId_email_key" ON "SubmissionOverride"("seasonId", "email");

-- CreateIndex
CREATE UNIQUE INDEX "VoterWhitelist_seasonId_email_key" ON "VoterWhitelist"("seasonId", "email");

-- CreateIndex
CREATE UNIQUE INDEX "AdminUser_email_key" ON "AdminUser"("email");

-- AddForeignKey
ALTER TABLE "Movie" ADD CONSTRAINT "Movie_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Vote" ADD CONSTRAINT "Vote_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "Vote" ADD CONSTRAINT "Vote_movieId_fkey" FOREIGN KEY ("movieId") REFERENCES "Movie"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "SubmissionOverride" ADD CONSTRAINT "SubmissionOverride_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season"("id") ON DELETE CASCADE ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE "VoterWhitelist" ADD CONSTRAINT "VoterWhitelist_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season"("id") ON DELETE CASCADE ON UPDATE CASCADE;

