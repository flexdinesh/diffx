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
import { codeMatchesShell, themesFor } from "./display-options.ts";
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
        *, *::before, *::after, ::-webkit-scrollbar-thumb { border-radius: 0; }
        :host, pre, code, [data-gutter], [data-content] {
          background-color: var(--code-surface, var(--diffs-bg));
        }
        /* Pierre's base colors only resolve inside this shadow root. */
        :host {
          --diffs-fg-number-deletion-override: color-mix(in lab, var(--diffs-deletion-base) var(--diff-number-base), var(--fg));
          --diffs-fg-number-addition-override: color-mix(in lab, var(--diffs-addition-base) var(--diff-number-base), var(--fg));
        }
        :is([data-line-type^="context"], [data-line-type^="change"]) {
          --diffs-computed-decoration-bg: var(--code-surface, var(--diffs-bg));
        }
        [data-line][data-line-type^="change"] span {
          color: light-dark(
            var(--diffs-token-light, var(--diffs-light)),
            color-mix(in lab, var(--diffs-token-dark, var(--diffs-dark)) var(--diff-syntax-base), var(--fg))
          );
        }
        :is([data-change-icon="change"], [data-change-icon="new"], [data-change-icon="deleted"], [data-change-icon="rename-pure"], [data-change-icon="rename-changed"]) {
          background-color: currentColor;
          mask-position: center;
          mask-repeat: no-repeat;
          mask-size: contain;
          & use { display: none; }
        }
        [data-change-icon="change"] { mask-image: var(--status-glyph-change); }
        [data-change-icon="new"] { mask-image: var(--status-glyph-new); }
        [data-change-icon="deleted"] { mask-image: var(--status-glyph-deleted); }
        :is([data-change-icon="rename-pure"], [data-change-icon="rename-changed"]) { mask-image: var(--status-glyph-moved); }
        [data-diffs-header] [data-additions-count] { color: var(--code-count-addition, var(--diffs-addition-base)); }
        [data-diffs-header] [data-deletions-count] { color: var(--code-count-deletion, var(--diffs-deletion-base)); }
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
    <div
      className="landing-diff"
      data-code-surface={
        codeMatchesShell(diffTheme, themePreference) ? "shell" : undefined
      }
    >
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
