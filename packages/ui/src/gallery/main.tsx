import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { TooltipProvider } from "@diffx/ui/components/ui/tooltip";
import { Gallery } from "./Gallery.tsx";
import "../typography.css";
import "../tokens.css";
import "../styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");
createRoot(root).render(
  <StrictMode>
    <TooltipProvider>
      <Gallery page={new URLSearchParams(location.search).get("page")} />
    </TooltipProvider>
  </StrictMode>,
);
