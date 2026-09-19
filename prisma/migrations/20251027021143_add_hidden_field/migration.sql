-- RedefineTables
PRAGMA defer_foreign_keys=ON;
PRAGMA foreign_keys=OFF;
CREATE TABLE "new_Movie" (
    "id" TEXT NOT NULL PRIMARY KEY,
    "seasonId" TEXT NOT NULL,
    "title" TEXT NOT NULL,
    "trailerUrl" TEXT,
    "submittedBy" TEXT,
    "submittedAt" DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "hidden" BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT "Movie_seasonId_fkey" FOREIGN KEY ("seasonId") REFERENCES "Season" ("id") ON DELETE CASCADE ON UPDATE CASCADE
);
INSERT INTO "new_Movie" ("id", "seasonId", "submittedAt", "submittedBy", "title", "trailerUrl") SELECT "id", "seasonId", "submittedAt", "submittedBy", "title", "trailerUrl" FROM "Movie";
DROP TABLE "Movie";
ALTER TABLE "new_Movie" RENAME TO "Movie";
CREATE UNIQUE INDEX "Movie_seasonId_title_key" ON "Movie"("seasonId", "title");
PRAGMA foreign_keys=ON;
PRAGMA defer_foreign_keys=OFF;
