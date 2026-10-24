import { SettingsIcon } from "lucide-react";
import {
  ChoiceList,
  ChoiceListGroup,
  ChoiceListItem,
} from "@/components/choice-list";
import { IconButton } from "@/components/icon-button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { ThemeSwatch } from "@/components/theme-swatch";
import { useAppearance } from "./appearance-context.tsx";
import { readThemePreference, swatchPalette, THEMES } from "./theme.ts";

const groups = [
  { group: "diffx", label: "Diffx" },
  { group: "omarchy", label: "Omarchy" },
] as const;

/** Browser-saved preferences, opened from the status bar's trailing edge. */
export function SettingsButton() {
  const { themePreference, setThemePreference } = useAppearance();
  return (
    <Popover>
      <PopoverTrigger
        render={
          <IconButton id="settings" label="Settings" size="icon-xs">
            <SettingsIcon aria-hidden="true" />
          </IconButton>
        }
      />
      <PopoverContent size="list">
        <PopoverHeader>
          <PopoverTitle>Settings</PopoverTitle>
        </PopoverHeader>
        <PopoverSection title="Theme" aria-label="Theme">
          <ChoiceList
            aria-label="Theme"
            value={themePreference}
            onValueChange={(value) =>
              setThemePreference(readThemePreference(value))
            }
          >
            {groups.map(({ group, label }) => (
              <ChoiceListGroup key={group} label={label}>
                {THEMES.filter((theme) => theme.group === group).map(
                  (theme) => (
                    <ChoiceListItem key={theme.id} value={theme.id}>
                      <ThemeSwatch
                        palettes={
                          theme.id === "system"
                            ? ["diffx-light", "diffx-dark"]
                            : [swatchPalette(theme.id)]
                        }
                      />
                      {theme.label}
                    </ChoiceListItem>
                  ),
                )}
              </ChoiceListGroup>
            ))}
          </ChoiceList>
        </PopoverSection>
      </PopoverContent>
    </Popover>
  );
}
