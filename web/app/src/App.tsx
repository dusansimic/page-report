import { Navigate, Route, Routes } from "react-router";

import { Layout } from "@/components/Layout";
import { Landing } from "@/routes/Landing";
import { Dashboard } from "@/routes/Dashboard";
import { Tokens } from "@/routes/Tokens";
import { NotFound } from "@/routes/NotFound";
import { RequireSession } from "@/components/RequireSession";

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Landing />} />
        <Route element={<RequireSession />}>
          <Route path="dashboard" element={<Dashboard />} />
          <Route path="tokens" element={<Tokens />} />
        </Route>
        {/* The server 404s unknown API paths; anything else lands here. */}
        <Route path="index.html" element={<Navigate to="/" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}
