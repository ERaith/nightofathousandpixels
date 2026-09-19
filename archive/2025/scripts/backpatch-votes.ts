// scripts/backpatch-votes.ts
// Script to backpatch email addresses to existing votes based on timestamps

import { PrismaClient } from '@prisma/client';

const prisma = new PrismaClient();

async function backpatchVotes() {
  console.log('Starting backpatch process...\n');

  // Get all votes without emails
  const votesWithoutEmail = await prisma.vote.findMany({
    where: {
      email: null,
    },
    include: {
      movie: {
        select: {
          title: true,
        },
      },
    },
    orderBy: {
      createdAt: 'asc',
    },
  });

  console.log(`Found ${votesWithoutEmail.length} votes without email addresses:\n`);

  votesWithoutEmail.forEach((vote, index) => {
    console.log(`${index + 1}. Vote ID: ${vote.id}`);
    console.log(`   Movie: ${vote.movie.title}`);
    console.log(`   Voted at: ${vote.createdAt.toLocaleString()}`);
    console.log(`   Device ID: ${vote.deviceId}`);
    console.log(`   IP Hash: ${vote.ipHash.slice(0, 12)}...`);
    console.log('');
  });

  // Get all voter whitelist entries with timestamps
  const voterWhitelist = await prisma.voterWhitelist.findMany({
    where: {
      hasVoted: true,
      votedAt: {
        not: null,
      },
    },
    orderBy: {
      votedAt: 'asc',
    },
  });

  console.log(`\nFound ${voterWhitelist.length} whitelist entries with vote timestamps:\n`);

  voterWhitelist.forEach((voter, index) => {
    console.log(`${index + 1}. Email: ${voter.email}`);
    console.log(`   Name: ${voter.name || 'N/A'}`);
    console.log(`   Voted at: ${voter.votedAt?.toLocaleString()}`);
    console.log('');
  });

  // Try to match based on timestamps (within 5 seconds)
  console.log('\n=== ATTEMPTING AUTOMATIC MATCHING (within 5 seconds) ===\n');

  const matches: { voteId: string; email: string; movie: string; votedAt: Date }[] = [];

  for (const vote of votesWithoutEmail) {
    const voteTime = vote.createdAt.getTime();

    // Find whitelist entry within 5 seconds
    const match = voterWhitelist.find((voter) => {
      if (!voter.votedAt) return false;
      const whitelistTime = voter.votedAt.getTime();
      const diff = Math.abs(voteTime - whitelistTime);
      return diff <= 5000; // 5 seconds
    });

    if (match) {
      matches.push({
        voteId: vote.id,
        email: match.email,
        movie: vote.movie.title,
        votedAt: vote.createdAt,
      });
      console.log(`✓ MATCH FOUND:`);
      console.log(`  Vote for "${vote.movie.title}" at ${vote.createdAt.toLocaleString()}`);
      console.log(`  → Email: ${match.email} (voted at ${match.votedAt?.toLocaleString()})`);
      console.log('');
    }
  }

  console.log(`\n=== SUMMARY ===`);
  console.log(`Total votes without email: ${votesWithoutEmail.length}`);
  console.log(`Automatically matched: ${matches.length}`);
  console.log(`Unmatched: ${votesWithoutEmail.length - matches.length}`);

  if (matches.length > 0) {
    console.log('\n=== PROPOSED UPDATES ===\n');
    matches.forEach((match, index) => {
      console.log(`${index + 1}. UPDATE Vote SET email = '${match.email}' WHERE id = '${match.voteId}';`);
      console.log(`   (${match.movie} - ${match.votedAt.toLocaleString()})\n`);
    });

    // Ask for confirmation
    console.log('\nTo apply these updates, uncomment the code below and run again:\n');
    console.log('/*');
    console.log('for (const match of matches) {');
    console.log('  await prisma.vote.update({');
    console.log('    where: { id: match.voteId },');
    console.log('    data: { email: match.email },');
    console.log('  });');
    console.log('  console.log(`✓ Updated vote ${match.voteId} with email ${match.email}`);');
    console.log('}');
    console.log('*/');
  }

  await prisma.$disconnect();
}

backpatchVotes().catch((error) => {
  console.error('Error:', error);
  process.exit(1);
});
