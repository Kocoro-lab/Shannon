import { NextResponse } from 'next/server';

export async function GET() {
  return NextResponse.json({
    status: 'healthy',
    service: 'shannon-dashboard',
    timestamp: new Date().toISOString(),
  });
}