import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'standalone',
  eslint: {
    // Allow production builds with ESLint warnings
    ignoreDuringBuilds: true,
  },
  typescript: {
    // Allow production builds with TypeScript errors for now
    ignoreBuildErrors: true,
  },
  webpack: (config) => {
    // Add support for importing Web Workers
    config.module.rules.push({
      test: /\.worker\.(js|ts)$/,
      type: 'asset/resource',
      generator: {
        filename: 'static/[hash][ext][query]',
      },
    });

    return config;
  },
};

export default nextConfig;
