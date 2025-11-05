/** @type {import('next').NextConfig} */
const nextConfig = {
  images: {
    unoptimized: true,
  },
  trailingSlash: true,
  // Proxy API requests to Go backend during development
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: 'http://localhost:8888/api/:path*',
      },
    ]
  },
}

// Only use static export and custom distDir for production builds
if (process.env.NEXT_BUILD_EXPORT === 'true') {
  nextConfig.output = 'export'
  nextConfig.distDir = '../internal/registry/api/ui/dist'
  // Remove rewrites for static export (not supported)
  delete nextConfig.rewrites
}

module.exports = nextConfig

