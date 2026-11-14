import type { LandingVisuals } from "../Landing.tsx";
import { AgentsDemo } from "./AgentsDemo.tsx";
import { BenchDemo } from "./BenchDemo.tsx";
import { CommentsDemo } from "./CommentsDemo.tsx";
import { GitHubDemo } from "./GitHubDemo.tsx";
import { HeroDiff } from "./HeroDiff.tsx";

/** The landing page's animated figures, built with the stageshow kit. */
export const landingVisuals: LandingVisuals = {
  hero: <HeroDiff />,
  bench: <BenchDemo />,
  github: <GitHubDemo />,
  comments: <CommentsDemo />,
  agents: <AgentsDemo />,
};
