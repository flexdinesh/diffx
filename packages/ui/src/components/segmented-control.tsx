import type { ReactNode } from "react";
import {
  ToggleGroup,
  ToggleGroupItem,
} from "@diffx/ui/components/ui/toggle-group";

export interface SegmentedOption<Value extends string> {
  value: Value;
  label: ReactNode;
  /** Required when the visible label is abbreviated or icon-only. */
  ariaLabel?: string;
}

/** A single-choice set of compact options inside one bordered control. */
export function SegmentedControl<Value extends string>({
  label,
  options,
  value,
  onValueChange,
  size = "xs",
  touch,
  flush,
  id,
  className,
  hidden,
}: {
  /** Accessible group name, e.g. "Diff layout". */
  label: string;
  options: readonly SegmentedOption<Value>[];
  value: Value;
  onValueChange: (value: Value) => void;
  size?: "xs" | "sm";
  touch?: boolean;
  /** Drops the frame's height below md so touch items fill a fixed row. */
  flush?: boolean;
  id?: string;
  className?: string;
  hidden?: boolean;
}) {
  return (
    <ToggleGroup
      id={id}
      className={className}
      aria-label={label}
      hidden={hidden}
      variant="segmented"
      size={size}
      touch={touch}
      flush={flush}
      value={[value]}
      onValueChange={(values) => {
        const next = options.find((option) => option.value === values[0]);
        if (next) onValueChange(next.value);
      }}
    >
      {options.map((option) => (
        <ToggleGroupItem
          key={option.value}
          value={option.value}
          aria-label={option.ariaLabel}
        >
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}
