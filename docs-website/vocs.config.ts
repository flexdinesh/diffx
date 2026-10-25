import { defineConfig } from "vocs/config";

export default defineConfig({
  title: "diffx",
  description: "Review Git changes in your browser with diffx.",
  renderStrategy: "full-static",
  sidebar: [
    { text: "Introduction", link: "/" },
    { text: "Getting started", link: "/getting-started" },
    {
      text: "Guides",
      items: [
        { text: "Usage", link: "/usage" },
        { text: "Reviewing changes", link: "/reviewing-changes" },
        { text: "Piped diffs", link: "/piped-diffs" },
      ],
    },
    {
      text: "Agents and remote",
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
