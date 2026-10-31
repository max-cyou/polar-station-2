import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Plus, SlidersHorizontal, Sun, X, LoaderCircle } from "lucide-react";
import { ColorPicker } from "./ColorPicker";
import { LightingMode } from "./LightingMode";

type Preset = { id: string; name: string; colors: string[]; alerts?: Record<string, string>; other?: Record<string, string>; modes?: Record<string, string[]> };
type Settings = { modes: Record<string, string[]>; customPresets: Preset[]; activePreset: string; alerts: Record<string, string>; other: Record<string, string>; alice: { color: string; enabled: boolean }; volume: { low: string; middle: string; high: string } };
const defaultPresets: Preset[] = [
  { id: "warm", name: "Тёплый вечер", colors: ["#ad60ee", "#80429c", "#be62d6", "#e59cf1"] },
  { id: "northern", name: "Северное сияние", colors: ["#71b9ed", "#3987b5", "#65cdbd", "#b5e8e3"] },
  { id: "sunset", name: "Закат", colors: ["#f58e75", "#f1bd67", "#ed7a8b", "#a663ca"] },
];
type Props = { onModeColorsChange: (modes: Record<string, string[]>) => void; currentMode: string; onAliceChange: (alice: Settings["alice"]) => void; onBrightnessChange: (brightness: number) => void };

export function LightingSettings({ onAliceChange, onBrightnessChange, currentMode, onModeColorsChange }: Props) {
  const [presetsOpen, setPresetsOpen] = useState(false);
  const presetsRoot = useRef<HTMLDivElement>(null);
  const presetsTrigger = useRef<HTMLButtonElement>(null);
  const presetOptions = useRef<(HTMLButtonElement | null)[]>([]);
  const [colorGroup, setColorGroup] = useState<"alice" | "volume" | "all" | "alerts" | "other" | "modes">("all");
  const colorsDialog = useRef<HTMLDialogElement>(null);
  const [brightness, setBrightness] = useState(100);
  const [savedBrightness, setSavedBrightness] = useState(100);
  const [brightnessPending, setBrightnessPending] = useState(false);
  const [settings, setSettings] = useState<Settings | null>(null);
  const presets = [...defaultPresets, ...(settings?.customPresets ?? [])];
  const [saved, setSaved] = useState<Settings | null>(null);
  const applyDialog = useRef<HTMLDialogElement>(null);
  const [applyState, setApplyState] = useState({ pending: false, applying: false, available: false, retryAfter: 0, error: "" });
  const refreshApply = useRef<() => void>(() => {});
  useEffect(() => {
    const controller = new AbortController();
    let timer = 0;
    let running = false;
    async function update() {
      if (running || controller.signal.aborted) return;
      running = true;
      window.clearTimeout(timer);
      try {
        const response = await fetch("/api/appearance/apply", { cache: "no-store", signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]) });
        if (response.ok) { const data = await response.json(); if (!controller.signal.aborted) setApplyState(data); }
      } catch { /* Keep the recovery indicator during a temporary connection loss. */ }
      finally { running = false; if (!controller.signal.aborted) timer = window.setTimeout(update, 2000); }
    }
    refreshApply.current = () => { void update(); };
    void update();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, []);
  async function applyNow() {
    applyDialog.current?.close();
    setError("");
    setApplyState(previous => ({ ...previous, applying: true }));
    try {
      const response = await fetch("/api/appearance/apply", { method: "POST", signal: AbortSignal.timeout(8000) });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Не удалось применить подсветку");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Не удалось применить подсветку"); }
    finally { refreshApply.current(); }
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/appearance", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]), cache: "no-store" })
      .then(response => { if (!response.ok) throw new Error(); return response.json(); })
      .then(data => {
        if (controller.signal.aborted) return;
        if (!data.alice || !data.volume) throw new Error();
        data.customPresets ??= []; data.activePreset ??= "";
        setSettings(data); setSaved(data); onAliceChange(data.alice); onModeColorsChange(data.modes);
        if (typeof data.brightness === "number") { setBrightness(data.brightness); setSavedBrightness(data.brightness); onBrightnessChange(data.brightness); }
      }).catch(() => { if (!controller.signal.aborted) setError("Не удалось загрузить настройки"); });
    return () => controller.abort();
  }, [onAliceChange, onBrightnessChange, onModeColorsChange]);
  useEffect(() => {
    if (!presetsOpen) return;
    const selected = presets.findIndex(preset => preset.id === settings?.activePreset);
    presetOptions.current[Math.max(0, selected)]?.focus();
    const closeOutside = (event: PointerEvent) => { if (!presetsRoot.current?.contains(event.target as Node)) setPresetsOpen(false); };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [presetsOpen]);
  const dirty = JSON.stringify(settings) !== JSON.stringify(saved);
  async function save() {
    if (!settings || busy || applyState.applying) return;
    setBusy(true); setError("");
    try {
      const response = await fetch("/api/appearance", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(settings), signal: AbortSignal.timeout(8000) });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Не удалось сохранить настройки");
      data.customPresets ??= []; data.activePreset ??= "";
        setSettings(data); setSaved(data); onAliceChange(data.alice); onModeColorsChange(data.modes);
      refreshApply.current();
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
  const selectedPreset = presets.find(preset => preset.id === settings?.activePreset) ?? presets.find(preset => settings && preset.colors.join() === [settings.alice.color, settings.volume.low, settings.volume.middle, settings.volume.high].join());
  function applyPreset(index: number) {
    setPresetsOpen(false);
    presetsTrigger.current?.focus();
    const colors = presets[index].colors;
    setSettings(previous => previous && { ...previous, activePreset: presets[index].id, modes: presets[index].modes ?? previous.modes, alerts: presets[index].alerts ?? previous.alerts, other: presets[index].other ?? previous.other, alice: { ...previous.alice, color: colors[0] }, volume: { ...previous.volume, low: colors[1], middle: colors[2], high: colors[3] } });
  }
  function createPreset() {
    setPresetsOpen(false);
    setSettings(previous => {
      if (!previous) return previous;
      const defaults = defaultPresets[0].colors;
      const duplicates = previous.customPresets.filter(preset => preset.colors.map(color => color.toLowerCase()).join() === defaults.join());
      const existing = duplicates.length >= 2 ? duplicates[duplicates.length - 1] : null;
      const custom = existing ?? { id: `custom-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`, name: `Свой пресет ${Math.max(0, ...previous.customPresets.map(preset => Number(preset.name.match(/(\d+)$/)?.[1]) || 0)) + 1}`, colors: [...defaults], modes: previous.modes, alerts: { timer: "#45d778", notification: "#45d778", call: "#45d778" }, other: { bluetooth: "#3584e4", pause: "#a45cf5", track: "#a45cf5", like: "#a45cf5", boot: "#a45cf5" } };
      if (!existing && previous.customPresets.length >= 64) return previous;
      return { ...previous, activePreset: custom.id, customPresets: existing ? previous.customPresets : [...previous.customPresets, custom], alice: { ...previous.alice, color: custom.colors[0] }, volume: { ...previous.volume, low: custom.colors[1], middle: custom.colors[2], high: custom.colors[3] }, modes: custom.modes ?? previous.modes, alerts: custom.alerts ?? previous.alerts, other: custom.other ?? previous.other };
    });
  }
  useEffect(() => {
    setSettings(previous => {
      if (!previous?.activePreset.startsWith("custom-")) return previous;
      const current = previous.customPresets.find(preset => preset.id === previous.activePreset);
      if (!current) return previous;
      const updated = { ...current, colors: [previous.alice.color, previous.volume.low, previous.volume.middle, previous.volume.high], alerts: previous.alerts, other: previous.other, modes: previous.modes };
      if (JSON.stringify(current) === JSON.stringify(updated)) return previous;
      return { ...previous, customPresets: previous.customPresets.map(preset => preset.id === current.id ? updated : preset) };
    });
  }, [settings?.activePreset, settings?.alice.color, settings?.volume.low, settings?.volume.middle, settings?.volume.high, settings?.alerts, settings?.other, settings?.modes]);
  function manageColors(group: "alice" | "volume" | "all" | "alerts" | "other" | "modes") { setColorGroup(group); colorsDialog.current?.showModal(); }
  return <div className="lighting-layout"><div className="lighting-stack"><LightingMode currentMode={currentMode} onConfigure={() => manageColors("modes")} /><section className="lighting-settings lighting-redesign" aria-label="Настройки подсветки">
    <header className="light-card-header"><h2>Подсветка</h2></header>
    {!settings ? <p className="lighting-message">{error || "Загрузка…"}</p> : <>
      <div className="light-controls"><div className="light-control-group">
      <div className="light-control-heading light-preset-heading"><span>Пресет</span></div>
      <div className="light-preset-row"><div className="light-preset-dropdown" ref={presetsRoot} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setPresetsOpen(false); }}>
        <button type="button" className="light-current-preset" ref={presetsTrigger} aria-label="Пресет подсветки" aria-haspopup="menu" aria-expanded={presetsOpen} aria-controls="lighting-presets-menu" onClick={() => setPresetsOpen(previous => !previous)} onKeyDown={event => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setPresetsOpen(true); } }}><span className="light-preset-dot" style={{ background: `linear-gradient(135deg, ${settings.alice.color}, ${settings.volume.high})` }} /><span>{selectedPreset?.name ?? "Своя палитра"}</span><ChevronDown size={14} className={presetsOpen ? "preset-chevron-open" : ""} /></button>
        {presetsOpen && <div className="light-preset-options" id="lighting-presets-menu" role="menu" aria-label="Пресеты подсветки" onKeyDown={event => {
          const current = presetOptions.current.findIndex(option => option === document.activeElement);
          let next = current;
          if (event.key === "ArrowDown") next = (current + 1) % presets.length;
          else if (event.key === "ArrowUp") next = (current + presets.length - 1) % presets.length;
          else if (event.key === "Home") next = 0;
          else if (event.key === "End") next = presets.length - 1;
          else if (event.key === "Escape") { event.preventDefault(); setPresetsOpen(false); presetsTrigger.current?.focus(); return; }
          else return;
          event.preventDefault(); presetOptions.current[next]?.focus();
        }}>{presets.map((preset, index) => <button type="button" role="menuitemradio" aria-checked={selectedPreset === preset} tabIndex={-1} key={preset.id} ref={element => { presetOptions.current[index] = element; }} onClick={() => applyPreset(index)}><span className="light-preset-dot" style={{ background: `linear-gradient(135deg, ${preset.colors[0]}, ${preset.colors[3]})` }} /><span>{preset.name}</span>{selectedPreset === preset && <Check size={14} />}</button>)}</div>}
      </div><button type="button" className="light-custom" onClick={createPreset}><Plus size={15} />Свой</button></div>
      </div><div className="light-control-group">
      <button type="button" className="light-palette-button" onClick={() => manageColors("all")}>
        <span className="light-palette-copy"><span>Настроить цвета</span><small>Свечение, звук и события</small></span>
        <span className="light-palette-preview" aria-hidden="true">{[settings.alice.color, settings.volume.low, settings.volume.middle, settings.volume.high].map((color, index) => <i key={index} style={{ background: color }} />)}</span>
        <SlidersHorizontal size={15} aria-hidden="true" />
      </button>
      </div></div>
      <footer className="lighting-actions light-footer">
        {(error || applyState.error) && <span className="light-status-error" role="status">{error || applyState.error}</span>}
        <div className="light-save-buttons">
          <button type="button" className="auth-button" disabled={!dirty || busy || applyState.applying} onClick={() => void save()}>{busy ? "Сохранение…" : "Сохранить"}</button>
          {(applyState.pending || applyState.applying) && <button type="button" className="auth-button light-apply-button" disabled={dirty || busy || applyState.applying || !applyState.available || applyState.retryAfter > 0} title={dirty ? "Сначала сохраните изменения" : applyState.retryAfter > 0 ? `Повторное применение через ${applyState.retryAfter} с` : "Перезапустить службу Алисы и применить цвета"} onClick={() => applyDialog.current?.showModal()}>{applyState.applying ? <><LoaderCircle size={13} className="light-apply-spinner" />Применение…</> : applyState.retryAfter > 0 ? `Применить через ${applyState.retryAfter} с` : "Применить сейчас"}</button>}
        </div>
      </footer>
    </>}
    <dialog className="auth-dialog light-apply-dialog" ref={applyDialog} aria-labelledby="light-apply-title">
      <div className="auth-dialog-heading"><h2 id="light-apply-title">Применить подсветку?</h2><button type="button" className="auth-close" aria-label="Закрыть" onClick={() => applyDialog.current?.close()}><X size={18} /></button></div>
      <p>Служба Алисы будет перезапущена. Музыка и ответы могут ненадолго прерваться.</p>
      <div className="power-dialog-actions"><button type="button" className="auth-button" onClick={() => applyDialog.current?.close()}>Позже</button><button type="button" className="auth-button" onClick={() => void applyNow()}>Применить сейчас</button></div>
    </dialog>
    <dialog className="auth-dialog light-management-dialog" ref={colorsDialog} aria-labelledby="light-management-title" onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) colorsDialog.current?.close(); } }}>
      <div className="auth-dialog-heading"><h2 id="light-management-title">{colorGroup === "alice" ? "Цвета свечения" : colorGroup === "volume" ? "Цвета звука" : colorGroup === "alerts" ? "Оповещения" : colorGroup === "other" ? "Прочие режимы" : colorGroup === "modes" ? "Цвета режимов" : "Своя палитра"}</h2><button type="button" className="auth-close" aria-label="Закрыть" onClick={() => colorsDialog.current?.close()}><X size={18} /></button></div>
      <div className="light-editor-tabs" aria-label="Группа цветов">{([["all", "Все"], ["alice", "Свечение"], ["volume", "Звук"], ["alerts", "Оповещения"], ["other", "Прочие"], ["modes", "Режимы"]] as const).map(([key, label]) => <button type="button" key={key} aria-pressed={colorGroup === key} onClick={() => setColorGroup(key)}>{label}</button>)}</div>
      {settings && <div className="light-editor-body">
        {([
          { id: "alice", title: "Свечение", note: "Только сайт", rows: [["color", "Цвет свечения", "Когда Алиса слушает и отвечает"]] },
          { id: "volume", title: "Цвета звука", note: "Сайт и колонка", rows: [["low", "Тихо", "Низкая громкость"], ["middle", "Средне", "Средняя громкость"], ["high", "Громко", "Высокая громкость"]] },
          { id: "alerts", title: "Оповещения", note: "Палитра", rows: [["timer", "Таймеры", "Сигнал и обратный отсчёт"], ["notification", "Уведомления", "Новые и ожидающие уведомления"], ["call", "Звонки", "Входящие, исходящие и соединение"]] },
          { id: "other", title: "Прочие режимы", note: "Палитра", rows: [["bluetooth", "Bluetooth", "Подключение и воспроизведение"], ["pause", "Пауза", "Остановка и продолжение музыки"], ["track", "Переключение", "Следующий и предыдущий трек"], ["like", "Лайк", "Оценка трека"], ["boot", "Загрузка", "Включение станции"]] },
        ] as const).filter(group => colorGroup === "all" || colorGroup === group.id).map(group => <section className="light-editor-group" key={group.id}>
          <div className="light-editor-group-heading"><h3>{group.title}</h3><span>{group.note}</span></div>
          {group.id === "alice" && <label className="light-editor-toggle"><span>Включить свечение</span><input type="checkbox" checked={settings.alice.enabled} onChange={event => setSettings({ ...settings, alice: { ...settings.alice, enabled: event.target.checked } })} /></label>}
          {group.rows.map(([key, label, description]) => {
            const color = group.id === "alice" ? settings.alice.color : group.id === "volume" ? settings.volume[key as "low" | "middle" | "high"] : settings[group.id][key];
            return <div className="light-edit-row" key={key}><span className="light-edit-swatch" style={{ background: color }} /><span className="light-edit-copy"><span>{label}</span><small>{description}</small></span><ColorPicker label={label} value={color} onChange={value => {
              if (group.id === "alice") setSettings({ ...settings, alice: { ...settings.alice, color: value } });
              else if (group.id === "volume") setColor(key as "low" | "middle" | "high", value);
              else setSettings({ ...settings, [group.id]: { ...settings[group.id], [key]: value } });
            }} /></div>;
          })}
        </section>)}
        {(colorGroup === "all" || colorGroup === "modes") && <section className="light-editor-group">
          <div className="light-editor-group-heading"><h3>Режимы подсветки</h3></div>
          {([["candle", "Свеча"], ["night", "Ночник"], ["lava", "Лава-лампа"]] as const).map(([key, label]) => <div className="light-mode-palette" key={key}>
            <div className="light-mode-palette-heading"><span>{label}</span><span className="light-palette-preview">{settings.modes[key].map((color, index) => <i key={index} style={{ background: color }} />)}</span></div>
            {settings.modes[key].map((color, index) => <div className="light-edit-row" key={index}><span className="light-edit-swatch" style={{ background: color }} /><span className="light-edit-copy"><span>{key === "lava" ? `Цвет ${index + 1}` : "Цвет свечения"}</span></span><ColorPicker label={`${label}${key === "lava" ? ` · Цвет ${index + 1}` : ""}`} value={color} onChange={value => setSettings(previous => previous && { ...previous, modes: { ...previous.modes, [key]: previous.modes[key].map((old, i) => i === index ? value : old) } })} /></div>)}
          </div>)}
        </section>}
      </div>}
      <button className="auth-button color-picker-done" type="button" onClick={() => colorsDialog.current?.close()}>Готово</button>
    </dialog>
  </section></div>
    <section className="brightness-control" aria-label="Яркость подсветки">
      <Sun size={18} aria-hidden="true" />
      <input type="range" min="0" max="100" step="1" aria-label="Яркость подсветки сайта и колонки" aria-valuetext={`${brightness}%`} value={brightness} disabled={!settings || brightnessPending} onChange={event => { const value = Number(event.target.value); setBrightness(value); onBrightnessChange(value); }} onPointerUp={() => void saveBrightness()} onKeyUp={event => { if (["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown"].includes(event.key)) void saveBrightness(); }} />
      <span>{brightness}%</span>
    </section>
  </div>;
}
