declare module "swagger-ui-dist/swagger-ui-es-bundle.js" {
  export interface SwaggerRequest {
    credentials?: string;
    [key: string]: unknown;
  }
  export interface SwaggerUiOptions {
    url: string;
    domNode: HTMLElement;
    deepLinking?: boolean;
    persistAuthorization?: boolean;
    docExpansion?: "list" | "full" | "none";
    defaultModelsExpandDepth?: number;
    tryItOutEnabled?: boolean;
    /** Called once Swagger UI has rendered the operations. */
    onComplete?: () => void;
    requestInterceptor?: (request: SwaggerRequest) => SwaggerRequest;
  }
  const SwaggerUIBundle: (options: SwaggerUiOptions) => unknown;
  export default SwaggerUIBundle;
}
