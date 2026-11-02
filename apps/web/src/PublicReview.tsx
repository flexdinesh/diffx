import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import { AppProvider } from "./app-state.tsx";
import { SidebarBackdrop } from "./App.tsx";
import { DiffWorkspace } from "./DiffWorkspace.tsx";
import { highlighterOptions, poolOptions } from "./diff-workers.ts";
import { FileExplorerNav } from "./FileExplorerNav.tsx";
import { Navigation } from "./Navigation.tsx";
import { PublicLayout } from "./PublicPullRequest.tsx";

/**
 * A ready public preview: the file sidebar and Pierre's diff in the preview
 * shell. Review marks, comments, copy and WebMCP are absent.
 */
export function PublicReview() {
  return (
    <WorkerPoolContextProvider
      poolOptions={poolOptions}
      highlighterOptions={highlighterOptions}
    >
      <AppProvider>
        <PublicLayout
          ready
          overlay={<SidebarBackdrop />}
          navigation={
            <Navigation>
              <FileExplorerNav />
            </Navigation>
          }
        >
          <DiffWorkspace />
        </PublicLayout>
      </AppProvider>
    </WorkerPoolContextProvider>
  );
}
