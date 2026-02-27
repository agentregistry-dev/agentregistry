import { defineConfig } from '@hey-api/openapi-ts'

export default defineConfig({
  input: '../openapi.yaml',
  output: {
    path: 'lib/api',
  },
  plugins: [
    '@hey-api/typescript',
    '@hey-api/sdk',
    {
      name: '@hey-api/client-fetch',
      bundle: true,
    },
  ],
})
