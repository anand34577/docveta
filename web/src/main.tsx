import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { Toaster } from "sonner";
import { queryClient } from "@/lib/queries";
import { router } from "@/router";
import { TooltipProvider } from "@/components/ui/overlay";
import { ConfirmHost } from "@/components/ui/confirm";
import { applyTheme, useUI } from "@/stores/ui";
import "./index.css";

applyTheme(useUI.getState().theme);

function ThemedToaster() {
  const theme = useUI((s) => s.theme);
  return <Toaster theme={theme} position="top-center" richColors closeButton toastOptions={{ duration: 5000 }} />;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
        <ConfirmHost />
        <ThemedToaster />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);

if ("serviceWorker" in navigator && import.meta.env.PROD) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => undefined);
  });
}
