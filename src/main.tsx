import React from "react";
import { createRoot } from "react-dom/client";
import { LayoutDashboard } from "lucide-react";
import "@fontsource-variable/golos-text";
import "./style.css";

function App() {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <nav aria-label="Основная навигация">
          <a href="#dashboard" className="nav-active" aria-current="page" aria-label="Дашборд" title="Дашборд">
            <LayoutDashboard size={18} />
          </a>
        </nav>
      </aside>
      <main id="dashboard" aria-label="Дашборд">
        <div className="dashboard-block">
          <div className="station-heading">
            <h1>Моя станция</h1>
            <p>Версия прошивки —</p>
          </div>
          <img className="station-image" src="/station.webp" alt="Яндекс Станция" />
        </div>
      </main>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
