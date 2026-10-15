import { FileDiffIcon, MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AccountButton } from "./account.tsx";
import { useAppearance } from "./appearance-context.tsx";
import { readThemePreference, type ThemePreference } from "./theme.ts";
import { useCompactChrome } from "./use-compact-chrome.ts";

// Workspace views; file and Git views join the diff view here.
const views = [{ id: "diff", label: "Diff", Icon: FileDiffIcon }];

/** Fixed view switcher left of the file sidebar, hidden in compact chrome. */
export function ActivityBar() {
  const compact = useCompactChrome();
  if (compact) return null;
  return (
    <nav className="activity-bar" aria-label="Views">
      <ul className="activity-views">
        {views.map(({ id, label, Icon }) => (
          <li key={id}>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-current="page"
              aria-label={label}
              title={label}
            >
              <Icon aria-hidden="true" />
            </Button>
          </li>
        ))}
      </ul>
      <div className="activity-actions">
        <ShellActions side="right" />
      </div>
    </nav>
  );
}

/** Theme and account actions: activity bar bottom, or header when compact. */
export function ShellActions({ side }: { side: "right" | "bottom" }) {
  return (
    <>
      <ThemeMenu side={side} />
      <AccountButton />
    </>
  );
}

function themeLabel(theme: ThemePreference) {
  if (theme === "light") return "Light";
  if (theme === "dark") return "Dark";
  return "System";
}

function ThemeMenu({ side }: { side: "right" | "bottom" }) {
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
      <DropdownMenuContent side={side} align="end" aria-label="Theme">
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
