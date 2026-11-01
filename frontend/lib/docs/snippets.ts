import source from "./examples.json";

/**
 * Docs examples. The JSON is the single source: the docs page renders it,
 * scripts/check-docs-examples.mjs validates it against the OpenAPI document in
 * CI, and the Playwright suite executes every rendered example against the
 * live sandbox API. Snippets below are generated, never hand-written, and use
 * placeholder credentials only.
 */
export interface DocsExample {
  id: string;
  section: string;
  title: string;
  description: string;
  method: string;
  path: string;
  headers: Record<string, string>;
  body: Record<string, unknown> | null;
  binds?: Record<string, string>;
  bodyBinds?: Record<string, string>;
  invalidBody?: boolean;
  expect: { status: number; body: unknown };
}

export interface DocsSource {
  unique: string[];
  supported: { method: string; path: string; note: string }[];
  planned: { name: string; note: string }[];
  examples: DocsExample[];
}

export const docsSource = source as unknown as DocsSource;

export const baseUrlVariable = "RAILVANTA_BASE_URL";
export const keyVariable = "RAILVANTA_SANDBOX_KEY";

/** `{payment_id}` becomes a language-specific variable reference. */
function camel(name: string): string {
  return name.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase());
}

function pathParts(path: string): (string | { name: string })[] {
  return path
    .split(/(\{[a-z_]+\})/)
    .filter(Boolean)
    .map((part) => (part.startsWith("{") ? { name: part.slice(1, -1) } : part));
}

function hasBody(example: DocsExample): boolean {
  return example.body !== null;
}

function allHeaders(example: DocsExample): [string, string][] {
  return [
    ["Authorization", "__AUTH__"],
    ...(hasBody(example) ? [["Content-Type", "application/json"]] : []),
    ...Object.entries(example.headers),
  ] as [string, string][];
}

export function curlSnippet(example: DocsExample): string {
  const url = pathParts(example.path)
    .map((part) =>
      typeof part === "string" ? part : `$${part.name.toUpperCase()}`,
    )
    .join("");
  const lines = [`curl -X ${example.method} "$${baseUrlVariable}${url}"`];
  for (const [name, value] of allHeaders(example)) {
    lines.push(
      name === "Authorization"
        ? `  -H "Authorization: Bearer $${keyVariable}"`
        : `  -H "${name}: ${value}"`,
    );
  }
  if (example.body !== null) {
    lines.push(`  -d '${JSON.stringify(example.body, null, 2)}'`);
  }
  return lines.join(" \\\n");
}

export function typescriptSnippet(example: DocsExample): string {
  const url = pathParts(example.path)
    .map((part) =>
      typeof part === "string" ? part : `\${${camel(part.name)}}`,
    )
    .join("");
  const headers = allHeaders(example).map(([name, value]) => {
    const key = /^[A-Za-z]+$/.test(name) ? name : JSON.stringify(name);
    return name === "Authorization"
      ? `    ${key}: \`Bearer \${process.env.${keyVariable}}\`,`
      : `    ${key}: ${JSON.stringify(value)},`;
  });
  const lines = [
    `const response = await fetch(\`\${process.env.${baseUrlVariable}}${url}\`, {`,
    `  method: "${example.method}",`,
    "  headers: {",
    ...headers,
    "  },",
  ];
  if (example.body !== null) {
    const body = JSON.stringify(example.body, null, 2).split("\n").join("\n  ");
    lines.push(`  body: JSON.stringify(${body}),`);
  }
  lines.push(
    "});",
    "// A timeout or network error does not tell you whether the request took effect.",
    "console.log(response.status, await response.json());",
  );
  return lines.join("\n");
}

export function goSnippet(example: DocsExample): string {
  const url = pathParts(example.path)
    .map((part) =>
      typeof part === "string" ? JSON.stringify(part) : camel(part.name),
    )
    .join(" + ");
  const lines = [
    "package main",
    "",
    'import (\n\t"fmt"\n\t"io"\n\t"net/http"\n\t"os"\n\t"strings"\n)',
    "",
    "func main() {",
  ];
  const ids = pathParts(example.path)
    .filter((part): part is { name: string } => typeof part !== "string")
    .map((part) => part.name);
  for (const id of ids)
    lines.push(`\t${camel(id)} := os.Getenv("${id.toUpperCase()}")`);
  const target = `os.Getenv("${baseUrlVariable}") + ${url}`;
  if (example.body !== null) {
    lines.push(
      `\tbody := strings.NewReader(\`${JSON.stringify(example.body)}\`)`,
      `\trequest, _ := http.NewRequest("${example.method}", ${target}, body)`,
    );
  } else {
    lines.push(
      `\trequest, _ := http.NewRequest("${example.method}", ${target}, nil)`,
    );
  }
  for (const [name, value] of allHeaders(example)) {
    lines.push(
      name === "Authorization"
        ? `\trequest.Header.Set("Authorization", "Bearer "+os.Getenv("${keyVariable}"))`
        : `\trequest.Header.Set("${name}", "${value}")`,
    );
  }
  lines.push(
    "\tresponse, err := http.DefaultClient.Do(request)",
    "\tif err != nil {",
    "\t\t// The request may or may not have taken effect; retry writes with the same Idempotency-Key.",
    "\t\tpanic(err)",
    "\t}",
    "\tdefer response.Body.Close()",
    "\tpayload, _ := io.ReadAll(response.Body)",
    "\tfmt.Println(response.StatusCode, string(payload))",
    "}",
  );
  return lines.join("\n");
}

export function examplesIn(section: string): DocsExample[] {
  return docsSource.examples.filter((example) => example.section === section);
}
