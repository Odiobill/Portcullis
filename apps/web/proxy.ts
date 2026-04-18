import createMiddleware from 'next-intl/middleware';
import { routing } from './i18n/routing';
import { NextRequest, NextResponse } from 'next/server';

const intlMiddleware = createMiddleware(routing);

export default function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  // 1. Check if the user is trying to access the dashboard
  const isDashboardPage = pathname.includes('/dashboard');
  const isLoginPage = pathname.includes('/login');

  if (isDashboardPage) {
    const session = req.cookies.get('portcullis_session');
    
    if (!session || session.value !== 'authenticated') {
      // Redirect to login, preserving the locale if possible
      const locale = pathname.split('/')[1] || 'en';
      const loginUrl = new URL(`/${locale}/login`, req.url);
      return NextResponse.redirect(loginUrl);
    }
  }

  // 2. Prevent accessing login page if already authenticated
  if (isLoginPage) {
    const session = req.cookies.get('portcullis_session');
    if (session?.value === 'authenticated') {
      const locale = pathname.split('/')[1] || 'en';
      const dashboardUrl = new URL(`/${locale}/dashboard`, req.url);
      return NextResponse.redirect(dashboardUrl);
    }
  }

  // Fallback to intl middleware
  return intlMiddleware(req);
}

export const config = {
  // Match only internationalized pathnames
  matcher: ['/', '/(it|en)/:path*']
};
