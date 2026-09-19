# Night of a Thousand Pixels 🎬👽

Alien-spooky themed movie voting app with seasons, dual themes, gamification features, and comprehensive admin controls.

## Tech Stack

- **Next.js 15** (App Router with Server Components)
- **Prisma ORM** (SQLite for dev, PostgreSQL for production)
- **Tailwind CSS** (Animated UI with dual themes)
- **TypeScript** (Type-safe throughout)
- **Session-based Auth** (JWT tokens in HTTPOnly cookies)
- **Docker** (Production-ready containerization)

## Features

### Public Features
- ✅ Submit movies (configurable limits per user)
- ✅ Vote on movies (change vote anytime before season lock)
- ✅ Real-time vote updates with SWR
- ✅ Countdown timer to season lock date
- ✅ Voting race progress bar with avatars
- ✅ Dual themes: Weyland-Yutani (green) / Bio-Pulse (purple)
- ✅ Content-safe mode (reduced motion)
- ✅ Device ID tracking for submissions/votes

### Admin Features
- ✅ Secure email/password authentication
- ✅ Season management (name, lock date, default limits)
- ✅ Movie moderation (hide/show/delete submissions)
- ✅ Vote management (view all votes, delete votes)
- ✅ Per-user submission limits
- ✅ Voter whitelist with tracking
- ✅ Comprehensive dashboard with 4 tabs

## Quick Start

### Local Development

```bash
# Install dependencies (using npm)
npm install

# Set up environment
cp .env.example .env

# Generate a secure cookie secret
node -e "console.log(require('crypto').randomBytes(32).toString('hex'))"
# Add the output to COOKIE_SECRET in .env

# Initialize database
npm run prisma:dev

# Create admin user
node -e "
const bcrypt = require('bcryptjs');
const { PrismaClient } = require('@prisma/client');
const prisma = new PrismaClient();
(async () => {
  const hash = await bcrypt.hash('admin123', 10);
  await prisma.adminUser.create({
    data: {
      email: 'admin@nightofpixels.com',
      passwordHash: hash,
      name: 'Admin'
    }
  });
  console.log('Admin created: admin@nightofpixels.com / admin123');
  await prisma.\$disconnect();
})();
"

# Start dev server
npm run dev
```

Visit:
- **App:** http://localhost:3000
- **Admin Login:** http://localhost:3000/admin/login
- **Credentials:** admin@nightofpixels.com / admin123

### Docker Development

```bash
# Start with Docker Compose (includes PostgreSQL)
docker-compose up app-dev

# Or build and run production image
docker-compose up app
```

## Deploy to AWS Fargate

See [DEPLOY.md](./DEPLOY.md) for the complete deployment guide.

**Quick facts:**
- **Time:** 60 minutes
- **Cost:** ~$30-50/month (pay only when running)
- **Benefits:** Auto-scaling, zero server management, high availability
- **Domain:** pixels.eraith.dev

## Admin Access

1. Navigate to `/admin/login`
2. Login with your admin credentials
3. Access the admin dashboard with 4 tabs:
   - **Settings:** Season configuration, voter whitelist
   - **Movies:** View/hide/delete submissions
   - **Voters:** View all votes, delete votes
   - **Limits:** Set custom submission limits per email

## Project Structure

```
nightofathousandpixels/
├── app/
│   ├── (site)/          # Main site pages
│   │   ├── components/  # React components
│   │   ├── page.tsx     # Home/voting page
│   │   ├── submit/      # Submit movie page
│   │   └── admin/       # Admin dashboard
│   └── api/             # API routes
├── lib/                 # Utility functions
├── prisma/              # Database schema
├── styles/              # Global CSS
└── public/              # Static assets
```

## Environment Variables

See `.env.example` for required environment variables.

## License

MIT
