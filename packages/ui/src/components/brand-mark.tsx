/**
 * The diffx mark: an X cut into rows of code, with one row removed and one
 * added. Slate rows follow the surrounding brand text. Always beside the name,
 * so it stays out of the accessibility tree.
 */
export function BrandMark() {
  return (
    <svg
      viewBox="0 0 512 512"
      aria-hidden="true"
      className="size-icon shrink-0"
    >
      <path
        fill="currentColor"
        d="M32 32L190.7 32L231.9 96L73.3 96ZM321.3 32L480 32L438.7 96L280.1 96ZM156 224L356 224L335.3 256L356 288L156 288L176.7 256ZM73.3 416L231.9 416L190.7 480L32 480ZM280.1 416L438.7 416L480 480L321.3 480Z"
      />
      <path
        fill="var(--brand-removed)"
        d="M42.8 142.2L365.6 114L330 181.4L89.6 202.4Z"
      />
      <path
        fill="var(--brand-added)"
        d="M182 330.6L422.4 309.6L469.2 369.8L146.4 398Z"
      />
    </svg>
  );
}
