import type { NextConfig } from "next";
import path from "node:path";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Produce a self-contained server bundle for the Docker image.
  output: "standalone",
  // Monorepo: trace workspace dependencies from the repo root.
  outputFileTracingRoot: path.resolve(process.cwd(), "../.."),
};

export default nextConfig;
