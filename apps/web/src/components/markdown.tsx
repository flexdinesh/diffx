import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

// Untrusted prose (pull request descriptions) renders as React elements only:
// raw HTML is dropped, unsafe URL schemes are removed by react-markdown, and
// images become links so viewing a review never loads remote content.
const components: Components = {
  h1: ({ children }) => (
    <h3 className="mt-4 mb-2 text-md font-semibold first:mt-0">{children}</h3>
  ),
  h2: ({ children }) => (
    <h4 className="mt-4 mb-2 text-md font-semibold first:mt-0">{children}</h4>
  ),
  h3: ({ children }) => (
    <h5 className="mt-3 mb-2 text-base font-semibold first:mt-0">{children}</h5>
  ),
  h4: ({ children }) => (
    <h6 className="mt-3 mb-2 text-base font-semibold first:mt-0">{children}</h6>
  ),
  h5: ({ children }) => (
    <h6 className="mt-3 mb-2 text-sm font-semibold first:mt-0">{children}</h6>
  ),
  h6: ({ children }) => (
    <h6 className="mt-3 mb-2 text-sm font-semibold first:mt-0">{children}</h6>
  ),
  p: ({ children }) => <p className="mt-0 mb-2">{children}</p>,
  ul: ({ children }) => (
    <ul className="mt-0 mb-2 list-disc pl-6">{children}</ul>
  ),
  ol: ({ start, children }) => (
    <ol className="mt-0 mb-2 list-decimal pl-6" start={start}>
      {children}
    </ol>
  ),
  li: ({ className, children }) => (
    <li
      className={cn(
        "mb-1",
        className?.includes("task-list-item") && "list-none",
      )}
    >
      {children}
    </li>
  ),
  a: ({ href, children }) => (
    <a
      className="text-accent underline underline-offset-(--space-1) hover:text-fg"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
    >
      {children}
    </a>
  ),
  img: ({ src, alt }) =>
    typeof src === "string" && src ? (
      <a
        className="text-accent underline underline-offset-(--space-1) hover:text-fg"
        href={src}
        target="_blank"
        rel="noopener noreferrer"
      >
        {alt ? `Image: ${alt}` : "Image"}
      </a>
    ) : null,
  blockquote: ({ children }) => (
    <blockquote className="mx-0 mt-0 mb-2 border-l border-separator pl-3 text-secondary">
      {children}
    </blockquote>
  ),
  code: ({ className, children }) => (
    <code
      className={cn(
        "rounded-sm bg-panel px-1 font-mono text-code",
        className?.includes("language-") && "bg-transparent px-0",
      )}
    >
      {children}
    </code>
  ),
  pre: ({ children }) => (
    <pre className="mt-0 mb-2 overflow-x-auto rounded-md border border-border bg-panel p-3 font-mono text-code [&>code]:bg-transparent [&>code]:px-0">
      {children}
    </pre>
  ),
  hr: () => <hr className="my-3 border-0 border-t border-separator" />,
  table: ({ children }) => (
    <div className="mb-2 overflow-x-auto">
      <table className="border-collapse text-sm">{children}</table>
    </div>
  ),
  th: ({ children }) => (
    <th className="border border-border px-2 py-1 text-left font-semibold">
      {children}
    </th>
  ),
  td: ({ children }) => (
    <td className="border border-border px-2 py-1">{children}</td>
  ),
  input: ({ checked }) => (
    <Checkbox
      className="mr-1 align-middle"
      checked={checked === true}
      disabled
      aria-label={checked ? "Completed task" : "Open task"}
    />
  ),
};

/** Renders untrusted GitHub-flavored markdown as review prose. */
export function MarkdownText({
  children,
  className,
}: {
  children: string;
  className?: string;
}) {
  return (
    <div
      data-slot="markdown"
      className={cn(
        "min-w-0 text-md leading-copy break-words text-fg",
        className,
      )}
    >
      <Markdown remarkPlugins={[remarkGfm]} skipHtml components={components}>
        {children}
      </Markdown>
    </div>
  );
}
