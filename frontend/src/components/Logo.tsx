/**
 * The SheetDiver mark: a datasheet page with a graph trace diving through
 * it, in the Diver palette (navy, sky blue, signal yellow).
 */
export function Logo({ size = 26 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" style={{ width: size, height: size, flex: "none" }}>
      <rect x="4" y="4" width="56" height="56" rx="14" fill="#0e1a33" />
      <path d="M20 13h17l9 9v29a3 3 0 0 1-3 3H20a3 3 0 0 1-3-3V16a3 3 0 0 1 3-3z" fill="#e4ecf5" />
      <path d="M37 13v7a2 2 0 0 0 2 2h7z" fill="#9aa9be" />
      <path d="M22 31h8M22 36h12M22 41h6" stroke="#9aa9be" strokeWidth="2.2" strokeLinecap="round" />
      <path d="M12 46c6 0 8-15 14-15s7 9 12 9 6-12 14-14" fill="none" stroke="#4fc3f7" strokeWidth="4" strokeLinecap="round" />
      <circle cx="52" cy="26" r="4" fill="#f5d76e" />
    </svg>
  );
}
