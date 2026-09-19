-- Backpatch votes with emails based on timestamp matching
-- Run this against the production database

-- Vote 1: Kelly voted for "Chopping Mall" at 04:21:37
UPDATE "Vote"
SET email = 'kellykohlhagen@gmail.com'
WHERE id = 'cmhcx3jxr000v4287igi31c17';

-- Vote 2: Adam voted for "Bad Taste" at 04:03:43
UPDATE "Vote"
SET email = 'rockwell.adam@gmail.com'
WHERE id = 'cmhcwgjwx000t4287spfzk9bn';

-- Vote 3: spartan9109 voted for "Squirm" at 04:03:19
UPDATE "Vote"
SET email = 'spartan9109@gmail.com'
WHERE id = 'cmhcwg15m000r4287iypht4c2';

-- Vote 4: Dave voted for "Chopping Mall" at 04:02:29
UPDATE "Vote"
SET email = 'dave.l.bosley@gmail.com'
WHERE id = 'cmhcweypi000l5xh9v1dhsneg';

-- Vote 5: Erik voted for "Squirm" at 03:59:04
UPDATE "Vote"
SET email = 'raithhtiar@gmail.com'
WHERE id = 'cmhcwakd5000j5xh9rngi7bco';

-- Vote 6: Andi voted for "Chopping Mall" at 03:57:36
UPDATE "Vote"
SET email = 'andrealndavis@gmail.com'
WHERE id = 'cmhcw8odv000h5xh9s0ji0g4k';

-- Vote 7: Karen voted for "Squirm" at 03:43:23
UPDATE "Vote"
SET email = 'kmoldy@gmail.com'
WHERE id = 'cmhcvqea0000h4287vwndsi47';

-- Vote 8: Kelsey voted for "Chopping Mall" at 03:38:27
UPDATE "Vote"
SET email = 'bruckskel@gmail.com'
WHERE id = 'cmhcvk1o1000f4287bdqy6rbr';

-- Verify the updates
SELECT
  v.id,
  v.email as vote_email,
  v."createdAt" as vote_time,
  m.title as movie_title,
  m."submittedBy" as movie_submitter
FROM "Vote" v
JOIN "Movie" m ON v."movieId" = m.id
WHERE v."seasonId" = 'cmhc9gv1u0000130iodayzgbg'
ORDER BY v."createdAt" DESC;
