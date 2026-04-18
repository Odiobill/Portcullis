import type { MetadataRoute } from 'next'

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: 'Portcullis',
    short_name: 'Portcullis',
    description: 'Secure frontend for public servers hosting multiple services',
    start_url: '/',
    display: 'standalone',
    background_color: '#05050f',
    theme_color: '#00f2ff',
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
