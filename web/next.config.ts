import type { NextConfig } from "next";
const config: NextConfig = {
  output: "standalone",
  agentRules: false,
  experimental: { proxyClientMaxBodySize: "21mb", proxyTimeout: 120000 },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_INTERNAL_URL || "http://127.0.0.1:8080"}/api/:path*`,
      },
    ];
  },
};
export default config;
