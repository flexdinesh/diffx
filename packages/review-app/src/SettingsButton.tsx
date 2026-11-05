import { SettingsIcon } from "lucide-react";
import { ChoiceList, ChoiceListItem } from "@diffx/ui/components/choice-list";
import { IconButton } from "@diffx/ui/components/icon-button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
} from "@diffx/ui/components/ui/popover";
import { ThemeSwatch } from "@diffx/ui/components/theme-swatch";
import { useAppearance } from "./appearance-context.tsx";
import { readThemePreference, swatchPalette, THEMES } from "./theme.ts";

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
            {THEMES.map((theme) => (
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
            ))}
          </ChoiceList>
        </PopoverSection>
      </PopoverContent>
    </Popover>
  );
}
