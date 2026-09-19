/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone", // Required for Docker
  experimental: {
    serverActions: {
      bodySizeLimit: "2mb",
    },
  },
  // Skip build-time database access
  env: {
    SKIP_ENV_VALIDATION: "1",
  },
};

export default nextConfig;
