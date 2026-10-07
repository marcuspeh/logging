import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { QueryPage } from "./pages/QueryPage";

// The Argos Logs component is the entire app — no header/nav chrome.
// The wrapper only provides page height (the log panel is h-full) and
// padding so the card isn't flush against the viewport edges.
export function App() {
  return (
    <BrowserRouter>
      <div className="min-h-screen bg-slate-50 text-slate-900 font-sans">
        <div className="h-screen p-6">
          <Routes>
            <Route path="/" element={<QueryPage />} />
            <Route path="logs" element={<QueryPage />} />
            <Route path="query" element={<QueryPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </div>
    </BrowserRouter>
  );
}