import { useEffect, useState } from "react";

type Space = { total: number; used: number; available?: number };
type SystemData = { memory: Space | null; cpu: number | null; temperature: number | null; root: Space | null; data: Space | null };
function size(bytes: number) { return bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(1)} ГБ` : `${Math.round(bytes / 1024 ** 2)} МБ`; }
function space(value: Space | null) { return value ? `${size(value.used)} / ${size(value.total)}` : "—"; }

export function SystemInfo() {
  const [data, setData] = useState<SystemData | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    let timer = 0;
    async function update() {
      try {
        const response = await fetch("/api/system", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]), cache: "no-store" });
        if (!response.ok) throw new Error();
        const result = await response.json();
        if (!controller.signal.aborted) setData(result);
      } catch { if (!controller.signal.aborted) setData(null); }
      finally { if (!controller.signal.aborted) timer = window.setTimeout(update, 3000); }
    }
    void update();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, []);
  return <section className="dashboard-lower-station system-info" aria-label="Система">
    <h2>Система</h2>
    <dl>
      <div><dt>Процессор</dt><dd>{data?.cpu != null ? `${Math.round(data.cpu)}%` : "—"}</dd></div>
      <div><dt>Память</dt><dd>{space(data?.memory ?? null)}</dd></div>
      <div><dt>Температура</dt><dd>{data?.temperature != null ? `${Math.round(data.temperature)} °C` : "—"}</dd></div>
      <div><dt>Система</dt><dd>{space(data?.root ?? null)}</dd></div>
      <div><dt>Данные</dt><dd>{space(data?.data ?? null)}</dd></div>
    </dl>
  </section>;
}
