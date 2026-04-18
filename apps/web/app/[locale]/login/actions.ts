'use server';

import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';

export async function login(prevState: any, formData: FormData) {
  const passcode = formData.get('passcode') as string;
  const locale = formData.get('locale') as string || 'en';
  const correctPasscode = process.env.PORTCULLIS_PASSCODE;

  if (passcode === correctPasscode) {
    const cookieStore = await cookies();
    
    // Set authentication session
    cookieStore.set('portcullis_session', 'authenticated', {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: 60 * 60 * 24 * 7, // 1 week
      path: '/',
    });

    // Persist locale preference
    cookieStore.set('NEXT_LOCALE', locale, {
      path: '/',
      maxAge: 60 * 60 * 24 * 365, // 1 year
    });

    return { success: true };
  }

  return { success: false, error: 'Invalid passcode' };
}
