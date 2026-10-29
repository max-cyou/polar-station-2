import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { AudioLines, CircleAlert, Ellipsis, LayoutDashboard, LoaderCircle, Mic, Volume2, X } from "lucide-react";
import "@fontsource-variable/golos-text";
import "./style.css";
import { MatrixDisplay } from "./components/MatrixDisplay";
import { MusicCard } from "./components/MusicCard";
import { MusicQueue } from "./components/MusicQueue";

function App() {
  const [queueTrackId, setQueueTrackId] = useState("");
  const [queueSwitching, setQueueSwitching] = useState(false);
  const [uptime, setUptime] = useState<number | null>(null);
  const [alicePending, setAlicePending] = useState(false);
  const [aliceStarting, setAliceStarting] = useState(false);
  const [aliceState, setAliceState] = useState("unknown");
  const [musicPlaying, setMusicPlaying] = useState(false);
  const [volumePulse, setVolumePulse] = useState<{ id: number; color: string; durationMs: number } | null>(null);
  const [volume, setVolume] = useState<number | null>(null);
  const [volumeDraft, setVolumeDraft] = useState<number | null>(null);
  const lastVolumePulse = useRef<number | null>(null);
  const stateSequence = useRef(0);
  const waitingForConfirmation = useRef<number | null>(null);
  const aliceActive = ["listening", "busy", "speaking"].includes(aliceState);
  const aliceLabel = aliceStarting ? "Вызываю Алису" : aliceState === "listening" ? "Алиса слушает" : aliceState === "speaking" ? "Алиса говорит" : aliceState === "busy" ? "Алиса думает" : "Вызвать Алису";
  const [aliceError, setAliceError] = useState("");
  useEffect(() => {
    const events = new EventSource("/api/alice/events");
    events.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (!data || !["idle", "listening", "busy", "speaking", "unknown"].includes(data.state) || typeof data.available !== "boolean" || typeof data.sequence !== "number") return;
        stateSequence.current = data.sequence;
        setAliceState(data.available ? data.state : "unknown");
        setMusicPlaying(data.musicPlaying === true);
        if (data.volumeKnown && typeof data.volume === "number") setVolume(data.volume);
        const pulse = data.volumePulse;
        if (pulse && typeof pulse.id === "number") {
          if (lastVolumePulse.current !== null && pulse.id > lastVolumePulse.current && typeof pulse.ageMs === "number" && pulse.ageMs < 1500 && typeof pulse.color === "string" && /^#[0-9a-f]{6}$/i.test(pulse.color) && typeof pulse.durationMs === "number" && pulse.durationMs >= 100 && pulse.durationMs <= 3000) {
            setVolumePulse({ id: pulse.id, color: pulse.color, durationMs: pulse.durationMs });
          }
          lastVolumePulse.current = pulse.id;
        }
        if (waitingForConfirmation.current !== null && data.sequence > waitingForConfirmation.current && ["listening", "speaking"].includes(data.state) && data.available) {
          waitingForConfirmation.current = null;
          setAliceStarting(false);
        }
      } catch { /* Ignore malformed events; the stream reconnects on transport errors. */ }
    };
    events.onerror = () => { setAliceState("unknown"); setMusicPlaying(false); };
    return () => events.close();
  }, []);
  async function changeVolume(value: number) {
    setVolumeDraft(value);
    try {
      const response = await fetch("/api/music/command", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ command: "setVolume", volume: value / 10 }), signal: AbortSignal.timeout(10_000) });
      if (!response.ok) throw new Error();
      setVolume(value);
    } catch { setAliceError("Не удалось изменить громкость"); }
    finally { setVolumeDraft(null); }
  }
  useEffect(() => {
    if (!aliceError) return;
    const timer = window.setTimeout(() => setAliceError(""), 5000);
    return () => window.clearTimeout(timer);
  }, [aliceError]);
  async function wakeAlice() {
    if (alicePending || aliceActive) return;
    setAlicePending(true);
    setAliceStarting(true);
    waitingForConfirmation.current = stateSequence.current;
    setAliceError("");
    try {
      const response = await fetch("/api/alice/listen", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
        signal: AbortSignal.timeout(12_000),
      });
      if (!response.ok) throw new Error("Alice unavailable");
      if (waitingForConfirmation.current !== null) {
        setAliceError("Алиса не подтвердила прослушивание");
      }
    } catch {
      setAliceError("Не удалось вызвать Алису");
    } finally {
      setAlicePending(false);
      setAliceStarting(false);
      waitingForConfirmation.current = null;
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    async function loadUptime() {
      try {
        const response = await fetch("/api/station/uptime", { signal: controller.signal, cache: "no-store" });
        if (!response.ok) throw new Error();
        const data = await response.json();
        if (typeof data.uptime !== "number" || !Number.isFinite(data.uptime) || data.uptime < 0) throw new Error();
        setUptime(data.uptime);
      } catch { if (!controller.signal.aborted) setUptime(null); }
    }
    void loadUptime();
    const interval = window.setInterval(loadUptime, 60_000);
    return () => { controller.abort(); window.clearInterval(interval); };
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
        <div className={`dashboard-block${aliceActive ? " alice-calling" : ""}${aliceState === "speaking" ? " alice-speaking" : aliceState === "busy" ? " alice-thinking" : ""}`}>
          <div className="alice-speech-halo" aria-hidden="true" />
          {volumePulse && <div key={volumePulse.id} className="volume-pulse" aria-hidden="true" style={{ "--volume-color": volumePulse.color, animationDuration: `${volumePulse.durationMs}ms` } as React.CSSProperties} onAnimationEnd={() => setVolumePulse(previous => previous?.id === volumePulse.id ? null : previous)} />}
          <div className="station-heading">
            <h1>Моя станция</h1>
            <p className="station-uptime">{uptime === null ? "Время работы недоступно" : `Работает ${Math.floor(uptime / 86400) ? `${Math.floor(uptime / 86400)} д ` : ""}${Math.floor(uptime / 3600) % 24} ч ${Math.floor(uptime / 60) % 60} мин`}</p>
          </div>
          <MatrixDisplay active={aliceActive} musicPlaying={musicPlaying} />
            <button className="alice-button" type="button" aria-label={aliceLabel} title={aliceLabel} onClick={wakeAlice} disabled={alicePending || aliceActive} aria-busy={aliceStarting} data-state={aliceState}>
              <span className="alice-icon" data-active={aliceStarting} aria-hidden="true"><LoaderCircle className="alice-spinner" size={22} /></span>
              <span className="alice-icon" data-active={!aliceStarting && aliceState === "listening"} aria-hidden="true"><Mic size={23} strokeWidth={1.8} /></span>
              <span className="alice-icon" data-active={!aliceStarting && aliceState === "busy"} aria-hidden="true"><Ellipsis size={24} /></span>
              <span className="alice-icon" data-active={!aliceStarting && aliceState === "speaking"} aria-hidden="true"><AudioLines className="alice-voice" size={24} strokeWidth={1.8} /></span>
              <span className="alice-icon" data-active={!aliceStarting && !["listening", "busy", "speaking"].includes(aliceState)} aria-hidden="true"><AudioLines size={24} strokeWidth={1.8} /></span>
            </button>
        </div>
        <div className="dashboard-right">
          <MusicCard onTrackChange={setQueueTrackId} onTrackSwitching={setQueueSwitching} />
          <div className="dashboard-bottom">
          <section className="volume-card" aria-label="Громкость">
            <Volume2 size={18} aria-hidden="true" />
            <input type="range" min="0" max="10" step="0.5" value={volumeDraft ?? volume ?? 0} disabled={volume === null} aria-label="Громкость" onChange={event => setVolumeDraft(Number(event.target.value))} onPointerUp={event => void changeVolume(Number(event.currentTarget.value))} onKeyUp={event => { if (["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown"].includes(event.key)) void changeVolume(Number(event.currentTarget.value)); }} />
            <span>{volumeDraft ?? volume ?? "—"}</span>
          </section>
          <MusicQueue trackId={queueTrackId} switching={queueSwitching} />
          </div>
        </div>
      </main>
      {aliceError && (
        <div className="error-toast" role="alert">
          <CircleAlert size={18} aria-hidden="true" />
          <span>{aliceError}</span>
          <button type="button" className="toast-close" aria-label="Закрыть уведомление" onClick={() => setAliceError("")}>
            <X size={16} />
          </button>
        </div>
      )}
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
