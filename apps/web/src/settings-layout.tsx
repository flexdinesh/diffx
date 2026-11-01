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

/** Account settings sections in page order. */
const sections = [
  { id: "tokens", title: "API tokens" },
  { id: "github", title: "GitHub" },
  { id: "notifications", title: "Notifications" },
  { id: "password", title: "Password" },
] as const;

export type SettingsSectionId = (typeof sections)[number]["id"];

const RegistryContext = createContext<
  ((id: SettingsSectionId) => () => void) | null
>(null);

const sectionElement = (id: SettingsSectionId) =>
  document.getElementById(`settings-${id}`);

/**
 * The scrolling settings document: a sticky section nav beside the content.
 * Sections register as they render, since GitHub and Notifications stay
 * absent on servers without them; the nav lists only those present and marks
 * the one at the top of the viewport.
 */
export function SettingsLayout({ children }: { children: ReactNode }) {
  const scroller = useRef<HTMLElement>(null);
  const [mounted, setMounted] = useState<ReadonlySet<SettingsSectionId>>(
    new Set(),
  );
  const [current, setCurrent] = useState<SettingsSectionId | null>(null);
  const register = useCallback((id: SettingsSectionId) => {
    setMounted((ids) => new Set(ids).add(id));
    return () =>
      setMounted((ids) => {
        const next = new Set(ids);
        next.delete(id);
        return next;
      });
  }, []);
  const present = useMemo(
    () => sections.filter((section) => mounted.has(section.id)),
    [mounted],
  );
  useEffect(() => {
    const container = scroller.current;
    if (!container) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const top = container.getBoundingClientRect().top;
      const atEnd =
        container.scrollTop + container.clientHeight >=
        container.scrollHeight - 1;
      let next = present[0]?.id ?? null;
      for (const section of present) {
        const element = sectionElement(section.id);
        if (element && element.getBoundingClientRect().top - top <= 96)
          next = section.id;
      }
      if (atEnd && container.scrollTop > 0) next = present.at(-1)?.id ?? next;
      setCurrent(next);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    container.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      container.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
    };
  }, [present]);
  return (
    <RegistryContext.Provider value={register}>
      <main ref={scroller} className="settings-scroll">
        <div className="settings-page">
          <nav className="settings-nav" aria-label="Settings sections">
            <ul className="flex flex-col gap-1">
              {present.map((section) => (
                <li key={section.id}>
                  <LinkButton
                    variant="rail"
                    size="sm"
                    href={`#settings-${section.id}`}
                    aria-current={current === section.id ? "true" : undefined}
                    className="w-full justify-start"
                    onClick={(event) => {
                      event.preventDefault();
                      const element = sectionElement(section.id);
                      if (!element) return;
                      element.scrollIntoView({ block: "start" });
                      element
                        .querySelector("h2")
                        ?.focus({ preventScroll: true });
                      setCurrent(section.id);
                    }}
                  >
                    {section.title}
                  </LinkButton>
                </li>
              ))}
            </ul>
          </nav>
          <div className="min-w-0">{children}</div>
        </div>
      </main>
    </RegistryContext.Provider>
  );
}

/**
 * One settings section: its title and what it is for beside its controls on
 * wide screens, stacked on narrow ones.
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
  const register = useContext(RegistryContext);
  useEffect(() => register?.(id), [register, id]);
  const title = sections.find((section) => section.id === id)?.title;
  return (
    <section
      id={`settings-${id}`}
      className="settings-section"
      aria-labelledby={`${id}-title`}
    >
      <div>
        <h2
          id={`${id}-title`}
          tabIndex={-1}
          className="mb-2 text-md font-semibold"
        >
          {title}
        </h2>
        <p className="leading-copy text-secondary">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
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
