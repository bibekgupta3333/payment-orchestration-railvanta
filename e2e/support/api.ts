import SwaggerParser from "@apidevtools/swagger-parser";
import Ajv, { type AnySchema } from "ajv";
import addFormats from "ajv-formats";
import { resolve } from "node:path";
import { expect, type APIRequestContext, type APIResponse } from "@playwright/test";
import { state } from "./state";

interface ResponseDefinition {
  content?: Record<string, { schema?: AnySchema }>;
}
interface Operation {
  responses?: Record<string, ResponseDefinition>;
}
interface Contract {
  paths: Record<string, Record<string, Operation>>;
  components: { schemas: Record<string, AnySchema> };
}

const document = SwaggerParser.dereference(
  resolve(__dirname, "../../backend/api/openapi.yaml"),
).then((value) => value as unknown as Contract);
const ajv = new Ajv({ strict: false, allErrors: true });
addFormats(ajv);

export async function assertContract(response: APIResponse, method: string, path: string): Promise<void> {
  const spec = await document;
  const pathname = path.split("?")[0];
  const matched = Object.keys(spec.paths).find((candidate) => {
    const expression = candidate.replace(/\{[^}]+\}/g, "[^/]+");
    return new RegExp(`^${expression}$`).test(pathname);
  });
  const operation = matched ? spec.paths[matched][method.toLowerCase()] : undefined;
  const responses = operation?.responses;
  const definition = responses?.[String(response.status())] ?? responses?.default;
  let schema = definition?.content?.["application/json"]?.schema;
  if (!schema && response.status() >= 400) schema = spec.components.schemas.Error;
  expect(response.headers()["x-request-id"]).toBeTruthy();
  if (response.status() === 204) {
    expect(await response.text()).toBe("");
    return;
  }
  expect(schema, `Response schema for ${method} ${path} ${response.status()}`).toBeTruthy();
  const validator = ajv.compile(schema as AnySchema);
  const body: unknown = await response.json();
  expect(validator(body), JSON.stringify(validator.errors)).toBe(true);
  if (response.status() >= 400) {
    expect((body as { request_id: string }).request_id).toBe(response.headers()["x-request-id"]);
  }
}

interface CallOptions {
  key?: string | null;
  data?: Record<string, unknown> | string;
  headers?: Record<string, string>;
  origin?: string;
}

export class ApiClient {
  private readonly request: APIRequestContext;

  constructor(request: APIRequestContext) { this.request = request; }

  async call(method: string, path: string, options: CallOptions = {}): Promise<APIResponse> {
    const current = state();
    const key = options.key === undefined ? current.operatorKey : options.key;
    const response = await this.request.fetch(`${options.origin ?? current.apiOrigin}${path}`, {
      method,
      headers: {
        ...(key ? { Authorization: `Bearer ${key}` } : {}),
        ...options.headers,
      },
      ...(options.data === undefined ? {} : { data: options.data }),
    });
    await assertContract(response, method, path);
    return response;
  }

  async workspace(): Promise<{ id: string; name: string; environment: "sandbox" }> {
    const response = await this.call("GET", "/v1/workspace");
    expect(response.status()).toBe(200);
    return response.json();
  }
}
