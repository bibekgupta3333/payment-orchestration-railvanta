import { CopyCode } from "@/components/ui/copy-code";
import {
  curlSnippet,
  goSnippet,
  typescriptSnippet,
  type DocsExample,
} from "@/lib/docs/snippets";

/**
 * One runnable example. The data attributes are the machine-readable form that
 * the Playwright suite extracts and executes against the live sandbox API; they
 * hold only placeholder values (no credentials).
 */
export function ExampleBlock({ example }: { example: DocsExample }) {
  return (
    <article
      className="docs-example"
      aria-labelledby={`example-${example.id}`}
      data-docs-example=""
      data-example-id={example.id}
      data-method={example.method}
      data-path={example.path}
      data-expected-status={example.expect.status}
      data-request-headers={JSON.stringify(example.headers)}
      data-request-body={
        example.body === null ? "" : JSON.stringify(example.body)
      }
      data-binds={JSON.stringify(example.binds ?? {})}
      data-body-binds={JSON.stringify(example.bodyBinds ?? {})}
    >
      <h3 id={`example-${example.id}`}>{example.title}</h3>
      <p>{example.description}</p>
      <p className="docs-request">
        <span className="badge">{example.method}</span>{" "}
        <code className="mono">{example.path}</code>
        <span className="text-label">
          {" "}
          Expected status {example.expect.status}
        </span>
      </p>
      <CopyCode label={`cURL: ${example.title}`} code={curlSnippet(example)} />
      <details className="docs-more">
        <summary>TypeScript and Go</summary>
        <CopyCode
          label={`TypeScript: ${example.title}`}
          code={typescriptSnippet(example)}
        />
        <CopyCode label={`Go: ${example.title}`} code={goSnippet(example)} />
      </details>
      <details className="docs-more">
        <summary>Example response ({example.expect.status})</summary>
        <CopyCode
          label={`Response: ${example.title}`}
          code={JSON.stringify(example.expect.body, null, 2)}
        />
      </details>
    </article>
  );
}
