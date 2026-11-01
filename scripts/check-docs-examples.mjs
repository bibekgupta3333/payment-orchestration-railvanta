// CI check (WBS 4.4.9): every example on /docs must be valid against the OpenAPI
// document. It validates the shared source of the examples,
// frontend/lib/docs/examples.json, which the docs page renders and the
// Playwright suite executes against the live sandbox API.
//
// Checks: each example names a documented operation (method and path), supplies
// the required headers and a body that matches the request schema, expects a
// documented status whose example body matches the response schema, uses only
// /v1 sandbox paths and placeholder credentials; every supported endpoint exists
// in the contract; no planned endpoint does.
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(new URL("../e2e/package.json", import.meta.url));
const SwaggerParser = require("@apidevtools/swagger-parser");
const Ajv = require("ajv");
const addFormats = require("ajv-formats");

const contract = await SwaggerParser.dereference(
  new URL("../backend/api/openapi.yaml", import.meta.url).pathname,
);
const source = JSON.parse(
  readFileSync(new URL("../frontend/lib/docs/examples.json", import.meta.url), "utf8"),
);
const ajv = new Ajv({ strict: false, allErrors: true });
addFormats(ajv);

const problems = [];
const fail = (where, message) => problems.push(`${where}: ${message}`);

function findOperation(method, path) {
  const pathname = path.split("?")[0];
  for (const [template, item] of Object.entries(contract.paths)) {
    const expression = new RegExp(
      "^" + template.replace(/\{[^}]+\}/g, "[^/]+") + "$",
    );
    if (expression.test(pathname) && item[method.toLowerCase()]) {
      return {
        template,
        operation: item[method.toLowerCase()],
        parameters: [...(item.parameters ?? []), ...(item[method.toLowerCase()].parameters ?? [])],
      };
    }
  }
  return null;
}

function validate(where, schema, value) {
  const check = ajv.compile(schema);
  if (!check(value)) fail(where, JSON.stringify(check.errors));
}

const ids = new Set();
for (const example of source.examples) {
  const where = `example ${example.id}`;
  if (ids.has(example.id)) fail(where, "duplicate id");
  ids.add(example.id);
  if (!example.path.startsWith("/v1/")) fail(where, "path must be a /v1 sandbox path");
  if (example.path.includes("://")) fail(where, "path must be relative");
  const found = findOperation(example.method, example.path);
  if (!found) {
    fail(where, `${example.method} ${example.path} is not in the OpenAPI document`);
    continue;
  }
  const { operation, parameters } = found;
  const headers = Object.keys(example.headers ?? {}).map((name) => name.toLowerCase());
  for (const parameter of parameters) {
    if (parameter.in === "header" && parameter.required && !headers.includes(parameter.name.toLowerCase()))
      fail(where, `required header ${parameter.name} is missing`);
    if (parameter.in === "header" && parameter.schema)
      for (const [name, value] of Object.entries(example.headers ?? {}))
        if (name.toLowerCase() === parameter.name.toLowerCase())
          validate(`${where} header ${name}`, parameter.schema, value);
  }
  const declared = new Set(parameters.filter((p) => p.in === "query").map((p) => p.name));
  for (const name of new URLSearchParams(example.path.split("?")[1] ?? "").keys())
    if (!declared.has(name)) fail(where, `query parameter ${name} is not documented`);
  for (const name of headers)
    if (!parameters.some((p) => p.in === "header" && p.name.toLowerCase() === name))
      fail(where, `header ${name} is not documented`);
  for (const [name, value] of Object.entries(example.headers ?? {}))
    if (/^authorization$/i.test(name) || /\b(sk|rk)_(live|test)_/i.test(String(value)))
      fail(where, "examples must not embed credentials; the page adds placeholders");
  const requestSchema = operation.requestBody?.content?.["application/json"]?.schema;
  if (requestSchema) {
    if (example.body === null || example.body === undefined) fail(where, "request body is required");
    else if (example.invalidBody) {
      // A deliberate error example: the body must violate the schema, as documented.
      if (ajv.compile(requestSchema)(example.body)) fail(where, "invalidBody example actually satisfies the request schema");
    } else validate(`${where} request body`, requestSchema, example.body);
  } else if (example.body) fail(where, "this operation takes no request body");
  const response = operation.responses?.[String(example.expect.status)];
  if (!response) {
    fail(where, `status ${example.expect.status} is not documented`);
    continue;
  }
  const responseSchema = response.content?.["application/json"]?.schema;
  if (responseSchema) validate(`${where} response body`, responseSchema, example.expect.body);
}

for (const token of source.unique) {
  if (!JSON.stringify(source.examples).includes(token)) fail("unique", `${token} is not used by any example`);
}
for (const endpoint of source.supported) {
  const item = contract.paths[endpoint.path];
  if (!item?.[endpoint.method.toLowerCase()]) fail("supported", `${endpoint.method} ${endpoint.path} is not in the OpenAPI document`);
}
for (const path of source.plannedAbsentFromOpenApi) {
  if (contract.paths[path]) fail("planned", `${path} is documented as planned but exists in the OpenAPI document`);
}

if (problems.length > 0) {
  console.error("Docs examples do not match the OpenAPI document:\n" + problems.map((p) => `- ${p}`).join("\n"));
  process.exit(1);
}
console.log(`Docs examples valid against OpenAPI (${source.examples.length} examples, ${source.supported.length} supported endpoints).`);
