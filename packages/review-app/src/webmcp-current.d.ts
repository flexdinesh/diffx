import "webmcp-types";

declare global {
  namespace WebMCP {
    interface ToolAnnotations {
      consequentialHint?: boolean;
      debugging?: boolean;
    }
  }
}
