/** @type {import('next').NextConfig} */
const nextConfig = {
  images: {
    unoptimized: true,
  },
  async rewrites() {
    return [
      {
        source: '/v0/:path*',
        destination: 'http://localhost:12121/v0/:path*',
      },
      {
        source: '/api/:path*',
        destination: 'http://localhost:12121/api/:path*',
      },
    ]
  },
}

// Only use static export for production builds
if (process.env.NEXT_BUILD_EXPORT === 'true') {
  nextConfig.output = 'export'
  // Set basePath so all routes and assets are prefixed with /ui
  nextConfig.basePath = '/ui'
  nextConfig.assetPrefix = '/ui'
  // Disable trailingSlash for static export to avoid redirect loops
  nextConfig.trailingSlash = false
  // Remove rewrites for static export (not supported)
  delete nextConfig.rewrites
}

module.exports = nextConfig

