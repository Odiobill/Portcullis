import type { MetadataRoute } from 'next'

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: 'Portcullis',
    short_name: 'Portcullis',
    description: 'Self-hosted staging infrastructure manager',
    start_url: '/',
    display: 'standalone',
    background_color: '#0a0a0b',
    theme_color: '#3b82f6',
    icons: [
      {
        src: '/icon.png',
        sizes: '512x512',
        type: 'image/png',
      },
      {
        src: '/logo.png',
        sizes: '1024x1024',
        type: 'image/png',
      }
    ],
  }
}
