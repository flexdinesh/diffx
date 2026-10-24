import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { CheckIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { ListRow } from "@/components/list-row";
import { cn } from "@/lib/utils";

/**
 * A single choice among list rows (e.g. themes). Arrow keys move between rows
 * and select, like native radios; the checked row shares the selected fill.
 */
function ChoiceList({ className, ...props }: RadioGroup.Props<string>) {
  return (
    <RadioGroup
      data-slot="choice-list"
      className={cn("flex flex-col gap-0.5", className)}
      {...props}
    />
  );
}

/** Rows under a quiet heading that names the group for assistive tech. */
function ChoiceListGroup({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const labelId = useId();
  return (
    <div
      data-slot="choice-list-group"
      role="group"
      aria-labelledby={labelId}
      className="flex flex-col gap-0.5"
    >
      <div id={labelId} className="px-1 pt-2 pb-1 text-xs text-muted">
        {label}
      </div>
      {children}
    </div>
  );
}

function ChoiceListItem({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  return (
    <Radio.Root value={value} nativeButton render={<ListRow />}>
      {children}
      <Radio.Indicator className="ml-auto flex">
        <CheckIcon aria-hidden="true" />
      </Radio.Indicator>
    </Radio.Root>
  );
}

export { ChoiceList, ChoiceListGroup, ChoiceListItem };
