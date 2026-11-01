import { EllipsisIcon } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { LinkButton } from "@diffx/ui/components/link-button";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { navigate } from "./identity-context.tsx";

/** Account settings sections in nav order; each is its own page. */
const sections = [
  { id: "tokens", title: "API tokens" },
  { id: "github", title: "GitHub" },
  { id: "notifications", title: "Notifications" },
  { id: "password", title: "Password" },
] as const;

export type SettingsSectionId = (typeof sections)[number]["id"];

/** The path of one settings section's page. */
export const settingsPath = (id: SettingsSectionId) => `/settings/${id}`;

export const isSettingsPath = (path: string) =>
  path === "/settings" || path.startsWith("/settings/");

/** The section a settings path names; null for `/settings` and unknown pages. */
export function settingsSection(path: string): SettingsSectionId | null {
  const name = path.slice("/settings/".length);
  return sections.find((section) => section.id === name)?.id ?? null;
}

type Presence = "present" | "absent";

const SettingsContext = createContext<{
  current: SettingsSectionId | null;
  report: (id: SettingsSectionId, presence: Presence) => () => void;
} | null>(null);

/**
 * Settings pages: the account summary, a section nav marking the current
 * page, then that page's section. Every section stays mounted so sections can
 * share state, but only the current one shows. Sections report whether they
 * exist, since GitHub and Notifications stay absent on servers without them;
 * the nav lists those present, and `/settings`, unknown pages and absent
 * sections open the first section.
 */
export function SettingsLayout({
  section,
  header,
  children,
}: {
  section: SettingsSectionId | null;
  header: ReactNode;
  children: ReactNode;
}) {
  const scroller = useRef<HTMLElement>(null);
  const [known, setKnown] = useState<ReadonlyMap<SettingsSectionId, Presence>>(
    new Map(),
  );
  const report = useCallback((id: SettingsSectionId, presence: Presence) => {
    setKnown((ids) => new Map(ids).set(id, presence));
    return () =>
      setKnown((ids) => {
        const next = new Map(ids);
        next.delete(id);
        return next;
      });
  }, []);
  const present = useMemo(
    () => sections.filter((item) => known.get(item.id) === "present"),
    [known],
  );
  const missing = section === null || known.get(section) === "absent";
  useEffect(() => {
    if (!missing) return;
    const { search, hash } = window.location;
    navigate(`${settingsPath(sections[0].id)}${search}${hash}`, {
      replace: true,
    });
  }, [missing]);
  useEffect(() => {
    if (scroller.current) scroller.current.scrollTop = 0;
  }, [section]);
  const context = useMemo(
    () => ({ current: section, report }),
    [section, report],
  );
  return (
    <SettingsContext.Provider value={context}>
      <main ref={scroller} className="settings-scroll">
        <div className="settings-page">
          <div className="settings-header">{header}</div>
          <nav className="settings-nav" aria-label="Settings sections">
            <ul>
              {present.map((item) => (
                <li key={item.id}>
                  <LinkButton
                    variant="rail"
                    size="sm"
                    touch
                    href={settingsPath(item.id)}
                    aria-current={section === item.id ? "page" : undefined}
                    onClick={(event) => {
                      event.preventDefault();
                      if (section !== item.id) navigate(settingsPath(item.id));
                    }}
                  >
                    {item.title}
                  </LinkButton>
                </li>
              ))}
            </ul>
          </nav>
          <div className="settings-content">{children}</div>
        </div>
      </main>
    </SettingsContext.Provider>
  );
}

/**
 * One settings section: its title and what it is for beside its controls on
 * wide screens, stacked on narrow ones. Shown only on its own page.
 */
export function SettingsSection({
  id,
  description,
  children,
}: {
  id: SettingsSectionId;
  description: ReactNode;
  children: ReactNode;
}) {
  const context = useContext(SettingsContext);
  const report = context?.report;
  useEffect(() => report?.(id, "present"), [report, id]);
  const title = sections.find((section) => section.id === id)?.title;
  return (
    <section
      id={`settings-${id}`}
      className="settings-section"
      aria-labelledby={`${id}-title`}
      hidden={context !== null && context.current !== id}
    >
      <div>
        <h2 id={`${id}-title`} className="mb-2 text-md font-semibold">
          {title}
        </h2>
        <p className="leading-copy text-secondary">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
}

/** Marks a section this server or account does not have, so its page redirects. */
export function SettingsSectionAbsent({ id }: { id: SettingsSectionId }) {
  const report = useContext(SettingsContext)?.report;
  useEffect(() => report?.(id, "absent"), [report, id]);
  return null;
}

/**
 * A row's quiet actions behind one ghost menu, as in the console. Actions that
 * remove something confirm in a dialog after the menu closes.
 */
export function RowActions({
  label,
  disabled = false,
  actions,
}: {
  /** Names the trigger, such as “Actions for Laptop”. */
  label: string;
  disabled?: boolean;
  actions: { label: string; onSelect: () => void }[];
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            touch
            disabled={disabled}
            aria-label={label}
          />
        }
      >
        <EllipsisIcon aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {actions.map((action) => (
          <DropdownMenuItem key={action.label} onClick={action.onSelect}>
            {action.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
