import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
  // Transpile problematic packages for better compatibility
  transpilePackages: ['react-markdown', 'remark-gfm', 'rehype-highlight'],
};

export default nextConfig;
