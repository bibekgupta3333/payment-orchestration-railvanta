"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { shouldRetryRead } from "@/lib/api/client";
import { ThemeProvider } from "./theme";

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: {
    queries: { retry: shouldRetryRead, staleTime: 15_000 }, mutations: { retry: false },
  } }));
  return <QueryClientProvider client={client}><ThemeProvider>{children}</ThemeProvider></QueryClientProvider>;
}
