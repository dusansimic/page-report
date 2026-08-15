import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TransportProvider } from "@connectrpc/connect-query";
import { Toaster } from "sonner";

import { transport } from "@/lib/transport";
import { App } from "@/App";
import "@/index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Session and list state changes out of band (a CLI upload, a revoked
      // token), so prefer a refetch on focus over a long cache.
      staleTime: 10_000,
      retry: false,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
        <Toaster theme="dark" position="bottom-right" richColors />
      </QueryClientProvider>
    </TransportProvider>
  </StrictMode>,
);
