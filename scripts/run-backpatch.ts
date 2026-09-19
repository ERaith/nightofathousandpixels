// Run backpatch to update votes with email addresses
import { PrismaClient } from '@prisma/client';

const prisma = new PrismaClient();

async function backpatch() {
  console.log('Starting backpatch...\n');

  const updates = [
    { voteId: 'cmhcx3jxr000v4287igi31c17', email: 'kellykohlhagen@gmail.com', name: 'Kelly' },
    { voteId: 'cmhcwgjwx000t4287spfzk9bn', email: 'rockwell.adam@gmail.com', name: 'Adam' },
    { voteId: 'cmhcwg15m000r4287iypht4c2', email: 'spartan9109@gmail.com', name: 'spartan9109' },
    { voteId: 'cmhcweypi000l5xh9v1dhsneg', email: 'dave.l.bosley@gmail.com', name: 'Dave' },
    { voteId: 'cmhcwakd5000j5xh9rngi7bco', email: 'raithhtiar@gmail.com', name: 'Erik' },
    { voteId: 'cmhcw8odv000h5xh9s0ji0g4k', email: 'andrealndavis@gmail.com', name: 'Andi' },
    { voteId: 'cmhcvqea0000h4287vwndsi47', email: 'kmoldy@gmail.com', name: 'Karen' },
    { voteId: 'cmhcvk1o1000f4287bdqy6rbr', email: 'bruckskel@gmail.com', name: 'Kelsey' },
  ];

  for (const update of updates) {
    try {
      await prisma.vote.update({
        where: { id: update.voteId },
        data: { email: update.email },
      });
      console.log(`✓ Updated ${update.name} (${update.email})`);
    } catch (error: any) {
      console.error(`✗ Failed to update ${update.name}:`, error.message);
    }
  }

  console.log('\nBackpatch complete!');
  await prisma.$disconnect();
}

backpatch();
