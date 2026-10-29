import { useEffect, useState } from "react";
import { Sun } from "lucide-react";
import { ColorPicker } from "./ColorPicker";

type Settings = { alice: { color: string; enabled: boolean }; volume: { low: string; middle: string; high: string } };
type Props = { onAliceChange: (alice: Settings["alice"]) => void; onBrightnessChange: (brightness: number) => void };

export function LightingSettings({ onAliceChange, onBrightnessChange }: Props) {
  const [brightness, setBrightness] = useState(100);
  const [savedBrightness, setSavedBrightness] = useState(100);
  const [brightnessPending, setBrightnessPending] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [saved, setSaved] = useState<Settings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/appearance", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]), cache: "no-store" })
      .then(response => { if (!response.ok) throw new Error(); return response.json(); })
      .then(data => {
        if (controller.signal.aborted) return;
        if (!data.alice || !data.volume) throw new Error();
        setSettings(data); setSaved(data); onAliceChange(data.alice);
        if (typeof data.brightness === "number") { setBrightness(data.brightness); setSavedBrightness(data.brightness); onBrightnessChange(data.brightness); }
      }).catch(() => { if (!controller.signal.aborted) setError("Не удалось загрузить настройки"); });
    return () => controller.abort();
  }, [onAliceChange, onBrightnessChange]);
  const dirty = JSON.stringify(settings) !== JSON.stringify(saved);
  async function save() {
    if (!settings || busy) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const response = await fetch("/api/appearance", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(settings), signal: AbortSignal.timeout(8000) });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Не удалось сохранить настройки");
      setSettings(data); setSaved(data); onAliceChange(data.alice);
      setNotice("Сохранено. Цвета колонки применятся после перезагрузки.");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Не удалось сохранить настройки"); }
    finally { setBusy(false); }
  }
  function setColor(key: "low" | "middle" | "high", value: string) {
    setSettings(previous => previous && { ...previous, volume: { ...previous.volume, [key]: value } });
  }
  async function saveBrightness() {
    if (brightnessPending || brightness === savedBrightness) return;
    setBrightnessPending(true); setError("");
    try {
      const response = await fetch("/api/appearance/brightness", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ brightness }), signal: AbortSignal.timeout(8000) });
      if (!response.ok) throw new Error();
      setSavedBrightness(brightness);
    } catch { setBrightness(savedBrightness); onBrightnessChange(savedBrightness); setError("Не удалось сохранить яркость"); }
    finally { setBrightnessPending(false); }
  }
  return <div className="lighting-layout"><section className="lighting-settings" aria-label="Настройки подсветки">
    {!settings ? <p className="lighting-message">{error || "Загрузка…"}</p> : <>
      <div className="lighting-heading"><h2>Подсветка</h2></div>
      <div className="lighting-row"><div><span>Свечение Алисы</span><small>На сайте при обращении к станции</small></div><input type="checkbox" aria-label="Свечение Алисы" checked={settings.alice.enabled} onChange={e => setSettings({ ...settings, alice: { ...settings.alice, enabled: e.target.checked } })} /></div>

      <div className="lighting-row"><span>Цвет свечения</span><ColorPicker label="Цвет свечения Алисы" value={settings.alice.color} onChange={color => setSettings({ ...settings, alice: { ...settings.alice, color } })} /></div>
      <div className="lighting-subtitle">Пульс громкости</div>
      <div className="lighting-colors">{([["low", "Тихо"], ["middle", "Средне"], ["high", "Громко"]] as const).map(([key, label]) => <div className="lighting-color-item" key={key}><span>{label}</span><ColorPicker label={`Громкость: ${label}`} value={settings.volume[key]} onChange={color => setColor(key, color)} /></div>)}</div>
      <div className="lighting-actions"><span role="status">{error || notice}</span><button type="button" className="auth-button" disabled={!dirty || busy} onClick={() => void save()}>{busy ? "Сохранение…" : "Сохранить"}</button></div>
    </>}
  </section>
    <section className="brightness-control" aria-label="Яркость подсветки">
      <Sun size={18} aria-hidden="true" />
      <input type="range" min="0" max="100" step="1" aria-label="Яркость подсветки сайта и колонки" aria-valuetext={`${brightness}%`} value={brightness} disabled={!settings || brightnessPending} onChange={event => { const value = Number(event.target.value); setBrightness(value); onBrightnessChange(value); }} onPointerUp={() => void saveBrightness()} onKeyUp={event => { if (["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown"].includes(event.key)) void saveBrightness(); }} />
      <span>{brightness}%</span>
    </section>
  </div>;
}
