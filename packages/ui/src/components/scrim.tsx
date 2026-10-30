/**
 * Dims the page behind a mobile drawer and closes it on click. It stays out
 * of the tab order; keyboard users close the drawer with Escape or its own
 * close action.
 */
export function Scrim({
  label,
  onDismiss,
}: {
  label: string;
  onDismiss: () => void;
}) {
  return (
    <button
      type="button"
      data-slot="scrim"
      aria-label={label}
      tabIndex={-1}
      className="fixed inset-0 z-(--layer-backdrop) border-0 bg-backdrop"
      onClick={onDismiss}
    />
  );
}
