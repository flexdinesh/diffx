import { cn } from "@/lib/utils";

function SwatchStrip({ palette }: { palette: string }) {
  return (
    <span data-palette={palette} className="flex flex-1 bg-bg">
      <span className="flex-1" />
      <span className="w-1.5 bg-accent" />
      <span className="w-1.5 bg-success" />
      <span className="w-1.5 bg-error" />
    </span>
  );
}

/**
 * Previews a theme through its own tokens: `data-palette` scopes tokens.css,
 * so the strip shows that theme's surface, accent, success and error. Two
 * palettes split the swatch, e.g. Auto's light and dark.
 */
export function ThemeSwatch({
  palettes,
  className,
}: {
  palettes: readonly [string] | readonly [string, string];
  className?: string;
}) {
  return (
    <span
      data-slot="theme-swatch"
      aria-hidden="true"
      className={cn(
        "flex h-4 w-10 shrink-0 flex-col overflow-clip rounded-control ring-1 ring-border",
        className,
      )}
    >
      {palettes.map((palette) => (
        <SwatchStrip key={palette} palette={palette} />
      ))}
    </span>
  );
}
