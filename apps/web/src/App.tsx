import { Scrim } from "@/components/scrim";
import { useReviewState } from "./app-state.tsx";
import { OutdatedCommentsNotice } from "./CommentsPanel.tsx";
import { DiffWorkspace } from "./DiffWorkspace.tsx";
import { FileExplorerNav } from "./FileExplorerNav.tsx";
import { Header } from "./Header.tsx";
import { Navigation } from "./Navigation.tsx";
import { NewerSnapshotNotice } from "./ProjectsNav.tsx";
import { CopyDialog } from "./review.tsx";
import { capabilityEnabled, useSession } from "./session-context.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { WebMCPTools } from "./WebMCPTools.tsx";

export function App() {
  return (
    <>
      <WebMCPTools />
      <Header />
      <SidebarBackdrop />
      <div className="workbench">
        <Navigation>
          <FileExplorerNav />
        </Navigation>
        <div className="workspace">
          <NewerSnapshotNotice />
          <OutdatedCommentsNotice />
          <DiffWorkspace />
        </div>
      </div>
      <ReviewCopyDialog />
    </>
  );
}

function SidebarBackdrop() {
  const sidebar = useSidebarState();
  return sidebar.drawer === "files" ? (
    <Scrim label="Close review sidebar" onDismiss={sidebar.closeMobile} />
  ) : null;
}

function ReviewCopyDialog() {
  const review = useReviewState();
  const { capabilities } = useSession();
  return capabilityEnabled(capabilities.review.comments) &&
    review.copyContent !== null ? (
    <CopyDialog content={review.copyContent} onClose={review.closeCopy} />
  ) : null;
}
