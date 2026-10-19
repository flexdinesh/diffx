import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useAppearance } from "./appearance-context.tsx";
import { readThemePreference, type ThemePreference } from "./theme.ts";

function themeLabel(theme: ThemePreference) {
  if (theme === "light") return "Light";
  if (theme === "dark") return "Dark";
  return "System";
}

export function ThemeButton() {
  const { themePreference, setThemePreference } = useAppearance();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            id="theme"
            variant="ghost"
            size="icon"
            aria-label={`Theme: ${themeLabel(themePreference)}`}
            title={`Theme: ${themeLabel(themePreference)}`}
          />
        }
      >
        {themePreference === "system" ? (
          <MonitorIcon aria-hidden="true" />
        ) : themePreference === "dark" ? (
          <MoonIcon aria-hidden="true" />
        ) : (
          <SunIcon aria-hidden="true" />
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent side="bottom" align="end" aria-label="Theme">
        <DropdownMenuRadioGroup
          value={themePreference}
          onValueChange={(value: unknown) =>
            setThemePreference(readThemePreference(value))
          }
        >
          <DropdownMenuRadioItem value="light" closeOnClick>
            <SunIcon aria-hidden="true" /> Light
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark" closeOnClick>
            <MoonIcon aria-hidden="true" /> Dark
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system" closeOnClick>
            <MonitorIcon aria-hidden="true" /> System
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
