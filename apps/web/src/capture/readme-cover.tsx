import { ReadmeCover } from "@diffx/review-app/readme-cover";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

// The README cover at 1:1, filmed by scripts/record-readme-cover.ts.
const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");

createRoot(root).render(
  <StrictMode>
    <div id="cover" style={{ width: 1000 }}>
      <ReadmeCover />
    </div>
  </StrictMode>,
);
