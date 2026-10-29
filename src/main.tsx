import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { LayoutDashboard } from "lucide-react";
import "@fontsource-variable/golos-text";
import "./style.css";
import { StationModel } from "./components/StationModel";

function App() {
  const [firmware, setFirmware] = useState<string | null>(null);
  const [firmwareError, setFirmwareError] = useState(false);
  const firmwareParts = firmware?.split(".") ?? [];

  useEffect(() => {
    const controller = new AbortController();
    async function loadFirmware() {
      try {
        const response = await fetch("/api/station", {
          signal: controller.signal,
          cache: "no-store",
        });
        if (!response.ok) throw new Error("Firmware unavailable");
        const data: unknown = await response.json();
        if (!data || typeof data !== "object" || !("firmware" in data) || typeof data.firmware !== "string" || !data.firmware.trim()) {
          throw new Error("Invalid firmware response");
        }
        setFirmware(data.firmware);
        setFirmwareError(false);
      } catch {
        if (!controller.signal.aborted) setFirmwareError(true);
      }
    }
    void loadFirmware();
    const interval = window.setInterval(loadFirmware, 60_000);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, []);

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
            <p aria-live="polite">
              {firmwareError ? "Прошивка недоступна" : firmware ? (
                <>
                  {firmwareParts.slice(0, 5).join(".")}
                  {firmwareParts.length > 5 && <><br />{firmwareParts.slice(5).join(".")}</>}
                </>
              ) : "Загрузка…"}
            </p>
          </div>
          <StationModel />
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
