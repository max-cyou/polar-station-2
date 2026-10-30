import { useEffect, useRef, useState } from "react";
import { Ban, Flame, LoaderCircle, Moon, Settings as SettingsIcon, Waves } from "lucide-react";

export function LightingMode({ currentMode, onConfigure }: { currentMode: string; onConfigure: () => void }) {
  const [mode, setMode] = useState(currentMode);
  const [modePending, setModePending] = useState("");
  const [error, setError] = useState("");
  const modeBusy = useRef(false);
  useEffect(() => setMode(currentMode), [currentMode]);
  async function changeMode(next: string) {
    if (modeBusy.current || next === mode) return;
    modeBusy.current = true; setModePending(next); setError("");
    try {
      const response = await fetch("/api/appearance/mode", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: next }), signal: AbortSignal.timeout(28000) });
      const data = await response.json();
      if (!response.ok || data.ok !== true) throw new Error(data.error || "Не удалось переключить режим");
      setMode(data.mode);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Не удалось переключить режим"); }
    finally { modeBusy.current = false; setModePending(""); }
  }
  return <section className="lighting-mode-card" aria-label="Режим подсветки">
      <div className="light-control-heading light-mode-heading"><span>Режим</span><button type="button" className="light-editor-open light-mode-settings" aria-label="Настроить цвета режимов" onClick={onConfigure}><SettingsIcon size={14} /></button></div>
      <div className="light-modes" aria-label="Режим подсветки"><button type="button" aria-label="Без режима" title="Без режима" aria-pressed={mode === "none"} disabled={!!modePending} onClick={() => void changeMode("none")}>{modePending === "none" ? <LoaderCircle className="auth-loading" size={14} /> : <Ban size={14} />}</button>{([{ id: "candle", label: "Свеча", Icon: Flame }, { id: "lava", label: "Лава-лампа", Icon: Waves }, { id: "night", label: "Ночник", Icon: Moon }]).map(({ id, label, Icon }) => <button type="button" key={id} aria-pressed={mode === id} disabled={!!modePending} onClick={() => void changeMode(id)}>{modePending === id ? <LoaderCircle className="auth-loading" size={14} /> : <Icon size={14} />}<span>{label}</span></button>)}</div>
    {error && <p className="lighting-mode-error" role="alert">{error}</p>}
  </section>;
}
