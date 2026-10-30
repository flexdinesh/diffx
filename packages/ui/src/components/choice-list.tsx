import { Radio } from "@base-ui/react/radio";
import { RadioGroup } from "@base-ui/react/radio-group";
import { CheckIcon } from "lucide-react";
import type { ReactNode } from "react";
import { ListRow } from "@diffx/ui/components/list-row";
import { cn } from "@diffx/ui/lib/utils";

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

export { ChoiceList, ChoiceListItem };
