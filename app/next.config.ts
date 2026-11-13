import type { NextConfig } from "next";
import { HOME_PATH } from "./src/lib/edition";

const nextConfig: NextConfig = {
  // A self-contained server for the Docker image (deploy/compose); it needs no node_modules at runtime.
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  // Home is the Dashboard in Pro, the Assistant in Core. A config redirect avoids a page that only throws NEXT_REDIRECT during prerender.
  async redirects() {
    return [
      { source: "/", destination: HOME_PATH, permanent: false },
      { source: "/panel", destination: HOME_PATH, permanent: true },
    ];
  },
  turbopack: {
    // The real build gets no demo kit: its code, fixtures and demo account never reach the browser.
    resolveAlias: process.env.NEXT_PUBLIC_ATLAS_API === "live" ? { "@/lib/demo": "./src/lib/demo/none.ts" } : {},
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
