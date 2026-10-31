import Link from "next/link";

export default function NotFound() {
  return <main id="main-content" className="docs-content" tabIndex={-1}><p className="eyebrow">404</p><h1>Page not found</h1><p>This sandbox route does not exist.</p><Link href="/dashboard" className="button">Return to overview</Link></main>;
}
