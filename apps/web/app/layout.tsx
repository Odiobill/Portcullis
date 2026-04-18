import { ReactNode } from 'react';

// Since we have a `[locale]` dynamic segment, we need a root layout
// that just renders the children.
export default function RootLayout({ children }: { children: ReactNode }) {
  return children;
}
