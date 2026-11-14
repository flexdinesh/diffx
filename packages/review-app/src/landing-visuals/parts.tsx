import type { ReactNode } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Folder,
  FolderOpen,
  MessageSquarePlus,
  Plus,
  UnfoldVertical,
} from "lucide-react";
import { FileIcon } from "../file-navigation.tsx";

export { BrandMark } from "@diffx/ui/components/brand-mark";

/** GitHub's mark (Octicons mark-github, MIT). */
export function GitHubMark({ size = 16 }: { size?: number }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      aria-hidden="true"
      style={{ flex: "none" }}
    >
      <path
        fill="currentColor"
        d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.37A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"
      />
    </svg>
  );
}

const keywords = new Set([
  "async",
  "await",
  "catch",
  "const",
  "describe",
  "export",
  "for",
  "from",
  "function",
  "if",
  "import",
  "let",
  "new",
  "return",
  "throw",
  "try",
]);
const types = new Set(["Promise", "T", "void", "number", "string"]);
const tokenPattern =
  /("(?:[^"\\]|\\.)*"|`[^`]*`)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_$][\w$]*)|(\s+)|([^\sA-Za-z_$\d"`]+)/g;

/** A small TypeScript highlighter in Pierre's palette, for demo lines. */
export function Code({ text }: { text: string }) {
  const spans: ReactNode[] = [];
  let index = 0;
  for (const match of text.matchAll(tokenPattern)) {
    const [whole, string, number, word, space] = match;
    const key = index++;
    if (string !== undefined) {
      spans.push(
        <span key={key} className="t-str">
          {whole}
        </span>,
      );
    } else if (number !== undefined) {
      spans.push(
        <span key={key} className="t-num">
          {whole}
        </span>,
      );
    } else if (word !== undefined) {
      const after = text.slice((match.index ?? 0) + whole.length);
      const className = keywords.has(word)
        ? "t-kw"
        : types.has(word)
          ? "t-type"
          : after.startsWith("(") || after.startsWith("<")
            ? "t-fn"
            : "t-var";
      spans.push(
        <span key={key} className={className}>
          {whole}
        </span>,
      );
    } else if (space !== undefined) {
      spans.push(whole);
    } else {
      spans.push(
        <span key={key} className="t-punct">
          {whole}
        </span>,
      );
    }
  }
  return <>{spans}</>;
}

export type DiffLine =
  | { kind: "hunk"; text: string }
  | { kind: "ctx" | "add" | "del"; n: number; text: string };

export function DiffRow({
  line,
  target,
  anchored,
  hovered,
  gutterAdd,
}: {
  line: DiffLine;
  target?: string;
  anchored?: boolean;
  hovered?: boolean;
  gutterAdd?: boolean;
}) {
  if (line.kind === "hunk") {
    return (
      <div className="dx-row" data-kind="hunk">
        <span className="dx-num">
          <UnfoldVertical />
        </span>
        <span className="dx-code">{line.text}</span>
      </div>
    );
  }
  return (
    <div
      className="dx-row"
      data-kind={line.kind}
      data-target={target}
      data-anchored={anchored || undefined}
      data-hovered={hovered || undefined}
    >
      <span className="dx-num">
        {gutterAdd !== undefined && (
          <span className="dx-gutter-add" data-show={gutterAdd || undefined}>
            <Plus />
          </span>
        )}
        {line.n}
      </span>
      <span className="dx-code">
        <Code text={line.text} />
      </span>
    </div>
  );
}

export function FileHeader({
  path,
  plus,
  minus,
  reviewed,
  reviewedTarget,
}: {
  path: string;
  plus: number;
  minus: number;
  reviewed: boolean;
  reviewedTarget?: string;
}) {
  return (
    <div className="dx-file-header">
      <ChevronDown className="dx-chevron" />
      <FileIcon path={path} reviewed={false} />
      <span className="dx-file-path">{path}</span>
      <span className="dx-file-stats">
        {plus > 0 && <span className="dx-plus">+{plus}</span>}
        {minus > 0 && <span className="dx-minus">−{minus}</span>}
      </span>
      <span className="dx-reviewed" data-target={reviewedTarget}>
        <span className="dx-check" data-on={reviewed || undefined}>
          <Check />
        </span>
        Reviewed
      </span>
      <span className="dx-file-comment">
        <MessageSquarePlus />
      </span>
    </div>
  );
}

export type TreeEntry = {
  name: string;
  depth: number;
  folder?: "open" | "closed";
  status?: "M" | "A";
};

export function TreeNode({
  entry,
  selected,
  reviewed,
}: {
  entry: TreeEntry;
  selected?: boolean;
  reviewed?: boolean;
}) {
  if (entry.folder) {
    return (
      <div className="dx-node">
        <span className="dx-indent" style={{ width: entry.depth * 16 }} />
        {entry.folder === "open" ? <ChevronDown /> : <ChevronRight />}
        {entry.folder === "open" ? <FolderOpen /> : <Folder />}
        <span className="dx-node-name">{entry.name}</span>
      </div>
    );
  }
  return (
    <div className="dx-node" data-selected={selected || undefined}>
      <span className="dx-indent" style={{ width: 20 + entry.depth * 16 }} />
      <FileIcon path={entry.name} reviewed={false} />
      <span className="dx-node-name">{entry.name}</span>
      <span className="dx-node-end">
        <span className="dx-reviewed-mark" data-on={reviewed || undefined}>
          <Check />
        </span>
        <span className="dx-status" data-status={entry.status}>
          {entry.status}
        </span>
      </span>
    </div>
  );
}
