import { defineConfig } from "vocs/config";

const productionUrl = "https://docs.diffx.app";
const siteUrl = process.env.DIFFX_DOCS_URL || productionUrl;
const isProduction = siteUrl === productionUrl;

export default defineConfig({
  title: "diffx",
  description: "Review Git changes in your browser with diffx.",
  renderStrategy: "full-static",
  baseUrl: siteUrl,
  accentColor: "light-dark(#3f5fd6, #74ade8)",
  iconUrl: "/icon.svg",
  logoUrl: { light: "/logo-light.svg", dark: "/logo-dark.svg" },
  ogImageUrl: `${productionUrl}/og.png`,
  // Non-production hosts stay out of search indexes and point crawlers at
  // production. Vocs serializes `head` to the client as source text, so the
  // function cannot reference module variables; keep the URL inline.
  ...(isProduction
    ? {}
    : {
        head: (path: string) => ({
          canonical: `https://docs.diffx.app${path}`,
          meta: { robots: "noindex, nofollow" },
        }),
        sitemap: false,
      }),
  topNav: [{ text: "Open app", link: "https://diffx.app" }],
  sidebar: [
    { text: "Introduction", link: "/" },
    { text: "Getting started", link: "/getting-started" },
    {
      text: "Guides",
      items: [
        { text: "Usage", link: "/usage" },
        { text: "Reviewing changes", link: "/reviewing-changes" },
      ],
    },
    {
      text: "Server and agents",
      items: [
        { text: "Agent hooks and remote ingestion", link: "/agent-hooks" },
        { text: "GitHub pull requests", link: "/github-pull-requests" },
        { text: "Agent plugins", link: "/plugins" },
        { text: "MCP", link: "/mcp" },
        { text: "WebMCP", link: "/webmcp" },
      ],
    },
    {
      text: "Reference",
      items: [{ text: "API and data", link: "/api" }],
    },
  ],
  socials: [{ icon: "github", link: "https://github.com/flexdinesh/diffx" }],
});
