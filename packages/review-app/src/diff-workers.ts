import { registerCustomCSSVariableTheme } from "@pierre/diffs";
import type {
  WorkerInitializationRenderOptions,
  WorkerPoolOptions,
} from "@pierre/diffs/react";
import DiffWorker from "@pierre/diffs/worker/worker.js?worker";
import { PALETTE_CODE_THEME } from "./theme.ts";

// Palettes without a bundled Shiki theme color code through CSS variables
// defined in tokens.css. The pool resolves themes here and ships them to workers.
registerCustomCSSVariableTheme(PALETTE_CODE_THEME, {});

// Keep syntax tokenization off the UI thread and reuse one bounded worker pool.
export const poolOptions: WorkerPoolOptions = {
  workerFactory: () => new DiffWorker(),
  poolSize: Math.min(4, Math.max(1, (navigator.hardwareConcurrency || 2) - 1)),
};
export const highlighterOptions: WorkerInitializationRenderOptions = {
  theme: { light: "pierre-light", dark: "pierre-dark" },
};
