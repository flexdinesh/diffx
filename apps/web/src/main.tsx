import { ReviewApplication } from "@diffx/review-app";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");

createRoot(root).render(
  <StrictMode>
    <ReviewApplication platform="browser" />
  </StrictMode>,
);
