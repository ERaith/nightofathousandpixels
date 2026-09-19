#!/usr/bin/env node
const { PrismaClient } = require('@prisma/client');
const bcrypt = require('bcryptjs');

async function createAdmin() {
  const prisma = new PrismaClient();

  const email = process.env.ADMIN_EMAIL || 'raithhtiar@gmail.com';
  const password = process.env.ADMIN_PASSWORD || 'ChangeMe123!';
  const name = process.env.ADMIN_NAME || 'Admin';

  try {
    // Check if admin already exists
    const existing = await prisma.adminUser.findUnique({
      where: { email }
    });

    if (existing) {
      console.log(`Admin user ${email} already exists`);
      process.exit(0);
    }

    // Hash password
    const passwordHash = await bcrypt.hash(password, 10);

    // Create admin
    await prisma.adminUser.create({
      data: {
        email,
        passwordHash,
        name
      }
    });

    console.log(`✓ Admin user created: ${email}`);
    console.log(`Password: ${password}`);

  } catch (error) {
    console.error('Error creating admin:', error);
    process.exit(1);
  } finally {
    await prisma.$disconnect();
  }
}

createAdmin();
