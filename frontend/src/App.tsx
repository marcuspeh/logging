import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { QueryPage } from "./pages/QueryPage";

export function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<QueryPage />} />
          <Route path="/query" element={<QueryPage />} />
          <Route path="*" element={<QueryPage />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}
