// app/api/admin/edit-movie/route.ts
import { NextResponse } from 'next/server';
import { prisma } from '@/lib/db';
import { checkAdminAuth } from '@/lib/api-auth';
import { toEmbed } from '@/lib/trailer';

export async function PATCH(request: Request) {
  try {
    const auth = await checkAdminAuth();
    if (!auth.authorized) {
      return auth.response!;
    }

    const { movieId, title, description, trailerUrl } = await request.json();

    if (!movieId) {
      return NextResponse.json(
        { error: 'Movie ID is required' },
        { status: 400 }
      );
    }

    const updateData: any = {};

    if (title !== undefined) {
      updateData.title = title.trim();
    }

    if (description !== undefined) {
      updateData.description = description?.trim() || null;
    }

    if (trailerUrl !== undefined) {
      updateData.trailerUrl = trailerUrl ? toEmbed(trailerUrl) : null;
    }

    const movie = await prisma.movie.update({
      where: { id: movieId },
      data: updateData,
    });

    return NextResponse.json({ ok: true, movie });
  } catch (error) {
    console.error('Error editing movie:', error);
    return NextResponse.json(
      { error: 'Failed to edit movie' },
      { status: 500 }
    );
  }
}
