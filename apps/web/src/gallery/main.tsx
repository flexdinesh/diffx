import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Gallery } from "./Gallery.tsx";
import "@fontsource-variable/jetbrains-mono/wght.css";
import "../typography.css";
import "../tokens.css";
import "../style.css";

const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");
createRoot(root).render(
  <StrictMode>
    <TooltipProvider>
      <Gallery page={new URLSearchParams(location.search).get("page")} />
    </TooltipProvider>
  </StrictMode>,
);
