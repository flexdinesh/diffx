import { useEffect, useRef } from "react";

/**
 * A pane's resize boundary. `end` sits on the pane's right edge and grows it
 * rightward; `start` sits on its left edge and grows it leftward. Double-click
 * resets the width when `onReset` is given.
 */
export function PaneResizer({
  label,
  min,
  max,
  width,
  hidden = false,
  edge = "end",
  onResize,
  onReset,
}: {
  label: string;
  min: number;
  max: number;
  width: number;
  hidden?: boolean;
  edge?: "start" | "end";
  onResize: (width: number) => void;
  onReset?: () => void;
}) {
  const drag = useRef({ x: 0, width });
  const grow = edge === "end" ? 1 : -1;
  useEffect(
    () => () => {
      document.body.classList.remove("resizing-sidebar");
    },
    [],
  );
  return (
    <hr
      className="sidebar-resizer"
      data-edge={edge}
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={width}
      tabIndex={0}
      hidden={hidden}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        drag.current = { x: event.clientX, width };
        event.currentTarget.setPointerCapture(event.pointerId);
        document.body.classList.add("resizing-sidebar");
      }}
      onPointerMove={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          onResize(
            drag.current.width + grow * (event.clientX - drag.current.x),
          );
        }
      }}
      onPointerUp={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId))
          event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onLostPointerCapture={() =>
        document.body.classList.remove("resizing-sidebar")
      }
      onDoubleClick={onReset}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          onResize(width + grow * (event.key === "ArrowRight" ? 20 : -20));
        }
      }}
    />
  );
}
