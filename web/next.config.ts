import type { NextConfig } from "next";

// The dashboard is a static export served by `shortdesk serve --web web/out`,
// so it lives on the same origin as the API and the desk screens.
//
// SHORTDESK_BUILD=phone builds the agent's phone instead: the Phone Desk on the main
// desk serves it, and Shortdesk passes it through at /phone/.
const phone = process.env.SHORTDESK_BUILD === "phone";

const nextConfig: NextConfig = {
  output: "export",
  basePath: phone ? "/phone" : undefined,
  env: { NEXT_PUBLIC_SHORTDESK_BUILD: phone ? "phone" : "dashboard" },
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
