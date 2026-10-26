import type { NextConfig } from "next";

// The dashboard is a static export served by `shortdesk serve --web web/out`,
// so it lives on the same origin as the API and the desk screens.
const nextConfig: NextConfig = {
  output: "export",
  // /login -> out/login/index.html, which a plain file server resolves.
  trailingSlash: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
