import type { ReactNode } from "react";
import "./landing.css";

/** A landing section: copy on the left, its figure on the right. */
export function Feature({
  visual,
  children,
}: {
  visual: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="dx-feature">
      <div className="dx-feature-copy">{children}</div>
      <div className="dx-feature-visual">{visual}</div>
    </section>
  );
}
