import type { DiffLineAnnotation, FileDiffOptions } from "@pierre/diffs";
import { PatchDiff } from "@pierre/diffs/react";
import { useMemo } from "react";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@diffx/ui/components/ui/card";
import { useAppearance } from "./appearance-context.tsx";
import { themesFor } from "./display-options.ts";
import { demoComment, demoCommentLine, demoPatch } from "./landing-demo.ts";

/**
 * The landing page's demo review: Pierre renders the synthetic change with its
 * one static comment. Kept apart from Landing so the page composes without
 * loading Pierre's custom elements.
 */
export function LandingDemoDiff() {
  const { theme, themePreference, diffTheme } = useAppearance();
  const codeThemes = themesFor(diffTheme, themePreference);
  const options = useMemo<FileDiffOptions<true, undefined>>(
    () => ({
      theme: codeThemes,
      themeType: theme,
      diffStyle: "unified",
      overflow: "wrap",
      diffIndicators: "bars",
      hunkSeparators: "simple",
      unsafeCSS: `
        [data-diffs-header] { background: var(--diff-file-header); }
        [data-change-icon="change"] { color: var(--warning); }
      `,
    }),
    [codeThemes, theme],
  );
  const annotations = useMemo<DiffLineAnnotation<true>[]>(
    () => [{ side: "additions", lineNumber: demoCommentLine, metadata: true }],
    [],
  );
  return (
    <div className="landing-diff">
      <PatchDiff
        patch={demoPatch}
        options={options}
        lineAnnotations={annotations}
        renderAnnotation={() => <DemoComment />}
        disableWorkerPool
      />
    </div>
  );
}

function DemoComment() {
  return (
    <article className="landing-comment">
      <Card density="inline">
        <CardHeader>
          <CardTitle>
            <span className="font-medium text-fg">You</span>
            <span>
              Line {demoComment.start} · {demoComment.scope}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p>{demoComment.body}</p>
        </CardContent>
      </Card>
    </article>
  );
}
