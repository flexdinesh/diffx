import { createContext, useContext, type ReactNode } from "react";
import { useStreamNav } from "./use-stream-nav.ts";

const StreamNavContext = createContext<ReturnType<typeof useStreamNav> | null>(
  null,
);

export function StreamNavProvider({ children }: { children: ReactNode }) {
  const streams = useStreamNav();
  return <StreamNavContext value={streams}>{children}</StreamNavContext>;
}

export function useStreamNavState() {
  const streams = useContext(StreamNavContext);
  if (!streams) throw new Error("Page components require StreamNavProvider");
  return streams;
}
