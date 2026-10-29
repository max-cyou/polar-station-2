import { useEffect, useRef, useState } from "react";
import { CircleAlert, Heart, LoaderCircle, Pause, Play, QrCode, RefreshCw, SkipBack, SkipForward, X } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";

type AuthState = "checking" | "signedOut" | "signedIn" | "error";
type QrState = "loading" | "waiting" | "expired" | "error";
type MusicInfo = { trackId: string; connected: boolean; playing: boolean; title: string; artist: string; cover: string; duration: number; position: number; error?: string };
function musicTime(value: number) { return Number.isFinite(value) && value >= 0 ? `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, "0")}` : "--:--"; }

async function requestAuth(path: string, signal: AbortSignal, method = "GET") {
  const response = await fetch(path, {
    method,
    signal: AbortSignal.any([signal, AbortSignal.timeout(45_000)]),
    cache: "no-store",
    ...(method === "POST" ? { headers: { "Content-Type": "application/json" }, body: "{}" } : {}),
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) throw Object.assign(new Error(typeof data?.error === "string" ? data.error : "Не удалось связаться с сервером"), { status: response.status });
  if (!data || typeof data !== "object") throw new Error("Не удалось прочитать ответ сервера");
  return data;
}

export function MusicCard() {
  const [auth, setAuth] = useState<AuthState>("checking");
  const [checkVersion, setCheckVersion] = useState(0);
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const closeTimer = useRef(0);
  const closingRef = useRef(false);
  const [qrVersion, setQrVersion] = useState(0);
  const [qrState, setQrState] = useState<QrState>("loading");
  const [link, setLink] = useState("");
  const [error, setError] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const loginButton = useRef<HTMLButtonElement>(null);
  const playButton = useRef<HTMLButtonElement>(null);
  const authorized = auth === "signedIn";
  const [music, setMusic] = useState<MusicInfo | null>(null);
  const [position, setPosition] = useState(0);
  const [seek, setSeek] = useState<number | null>(null);
  const [commandPending, setCommandPending] = useState(false);
  const [pendingCommand, setPendingCommand] = useState("");
  const commandBusy = useRef(false);
  const commandEpoch = useRef(0);
  const [musicError, setMusicError] = useState("");
  const [liked, setLiked] = useState<boolean | null>(null);
  const [likePending, setLikePending] = useState(false);
  const [brokenCover, setBrokenCover] = useState("");
  const [adaptiveBackground, setAdaptiveBackground] = useState(true);
  const playback = useRef({ position: 0, duration: 0, playing: false, received: performance.now() });
  const progress = seek ?? position;
  const progressRatio = music?.duration ? Math.max(0, Math.min(1, progress / music.duration)) : 0;
  const connected = music?.connected === true && authorized;
  const wave = `M0 8 Q4 0 8 8 ${Array.from({ length: 39 }, (_, i) => `T${(i + 2) * 8} 8`).join(" ")}`;

  useEffect(() => () => window.clearTimeout(closeTimer.current), []);
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/appearance", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]), cache: "no-store" })
      .then(response => response.ok ? response.json() : null)
      .then(data => { if (!controller.signal.aborted && typeof data?.music?.adaptiveBackground === "boolean") setAdaptiveBackground(data.music.adaptiveBackground); })
      .catch(() => {});
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (!musicError) return;
    const timer = window.setTimeout(() => setMusicError(""), 5000);
    return () => window.clearTimeout(timer);
  }, [musicError]);

  useEffect(() => {
    if (!authorized) { setMusic(null); setPosition(0); setSeek(null); return; }
    const controller = new AbortController();
    let timer = 0;
    async function update() {
      const epoch = commandEpoch.current;
      try {
        const data = await requestAuth("/api/music", controller.signal);
        if (typeof data.connected !== "boolean" || typeof data.playing !== "boolean" || typeof data.duration !== "number" || typeof data.position !== "number") throw new Error("Некорректное состояние плеера");
        if (controller.signal.aborted) return;
        if (commandBusy.current || epoch !== commandEpoch.current) return;
        setMusic(data as MusicInfo);
        setPosition(data.position);
        playback.current = { position: data.position, duration: data.duration, playing: data.playing && data.connected, received: performance.now() };
      } catch (cause) {
        if (controller.signal.aborted) return;
        if ((cause as { status?: number }).status === 401) { setAuth("signedOut"); return; }
        setMusic(previous => ({ trackId: previous?.trackId ?? "", connected: false, playing: false, title: previous?.title ?? "", artist: previous?.artist ?? "", cover: previous?.cover ?? "", duration: previous?.duration ?? 0, position: previous?.position ?? 0, error: "Не удалось получить состояние плеера" }));
        playback.current.playing = false;
      } finally {
        if (!controller.signal.aborted) timer = window.setTimeout(update, 1000);
      }
    }
    void update();
    const progressTimer = window.setInterval(() => {
      const current = playback.current;
      let elapsed = current.position + (current.playing ? (performance.now() - current.received) / 1000 : 0);
      if (current.duration > 0) elapsed = Math.min(elapsed, current.duration);
      setPosition(elapsed);
    }, 250);
    return () => { controller.abort(); window.clearTimeout(timer); window.clearInterval(progressTimer); };
  }, [authorized]);

  useEffect(() => {
    setLiked(null);
    if (!authorized || !music?.trackId) return;
    const controller = new AbortController();
    fetch(`/api/music/like?track=${encodeURIComponent(music.trackId)}`, { signal: controller.signal, cache: "no-store" })
      .then(response => { if (!response.ok) throw new Error(); return response.json(); })
      .then(data => { if (!controller.signal.aborted && typeof data.liked === "boolean") setLiked(data.liked); })
      .catch(() => { if (!controller.signal.aborted) setMusicError("Не удалось получить отметку трека"); });
    return () => controller.abort();
  }, [authorized, music?.trackId]);

  async function toggleLike() {
    const id = music?.trackId;
    if (!id || liked === null || likePending) return;
    setLikePending(true);
    try {
      const response = await fetch(`/api/music/like?track=${encodeURIComponent(id)}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ liked: !liked }), signal: AbortSignal.timeout(15000) });
      if (!response.ok) throw new Error();
      const data = await response.json();
      setLiked(previous => previous === liked ? data.liked : previous);
    } catch { setMusicError("Не удалось изменить отметку трека"); }
    finally { setLikePending(false); }
  }

  async function sendCommand(command: string, target?: number) {
    if (!connected || commandBusy.current) return false;
    commandBusy.current = true;
    commandEpoch.current++;
    setCommandPending(true); setPendingCommand(command); setMusicError("");
    try {
      const response = await fetch("/api/music/command", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ command, ...(target !== undefined ? { position: target } : {}) }), signal: AbortSignal.timeout(16_000) });
      const data = await response.json();
      if (!response.ok || data.ok !== true) throw new Error(typeof data.error === "string" ? data.error : "Не удалось выполнить команду");
      if (data.music && typeof data.music.playing === "boolean") {
        setMusic(data.music as MusicInfo);
        setPosition(data.music.position);
        playback.current = { position: data.music.position, duration: data.music.duration, playing: data.music.playing && data.music.connected, received: performance.now() };
      }
      if (target !== undefined) { playback.current = { ...playback.current, position: target, received: performance.now() }; setPosition(target); }
      return true;
    } catch (cause) {
      setMusicError(cause instanceof Error ? cause.message : "Не удалось управлять плеером");
      return false;
    } finally { commandEpoch.current++; commandBusy.current = false; setCommandPending(false); setPendingCommand(""); }
  }
  async function commitSeek() { if (seek === null) return; await sendCommand("rewind", seek); setSeek(null); }

  useEffect(() => {
    const controller = new AbortController();
    let timer = 0;
    let checking = false;
    async function check() {
      if (checking) return;
      checking = true;
      try {
        const data = await requestAuth("/api/auth", controller.signal);
        if (typeof data.loggedIn !== "boolean") throw new Error("Некорректный ответ авторизации");
        if (!controller.signal.aborted) setAuth(data.loggedIn ? "signedIn" : "signedOut");
      } catch {
        if (!controller.signal.aborted) setAuth("error");
      } finally {
        checking = false;
        if (!controller.signal.aborted) timer = window.setTimeout(check, 60_000);
      }
    }
    void check();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [checkVersion]);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    let timer = 0;
    setQrState("loading"); setLink(""); setError("");
    async function poll() {
      try {
        const data = await requestAuth("/api/auth/qr", controller.signal);
        if (controller.signal.aborted) return;
        if (data.state === "done" && data.loggedIn === true) {
          setAuth("signedIn");
          closeLogin();
          setCheckVersion(value => value + 1);
          return;
        }
        if (data.state === "expired") { setQrState("expired"); return; }
        if (data.state !== "waiting") throw new Error("Не удалось проверить QR-код");
        timer = window.setTimeout(poll, 2000);
      } catch (cause) {
        if (controller.signal.aborted) return;
        setQrState("error");
        setError(cause instanceof Error ? cause.message : "Не удалось проверить вход");
      }
    }
    async function start() {
      try {
        const data = await requestAuth("/api/auth/qr", controller.signal, "POST");
        if (controller.signal.aborted) return;
        if (typeof data.link !== "string" || !data.link.startsWith("https://")) throw new Error("Сервер не выдал QR-код");
        setLink(data.link); setQrState("waiting");
        timer = window.setTimeout(poll, 2000);
      } catch (cause) {
        if (controller.signal.aborted) return;
        setQrState("error");
        setError(cause instanceof Error ? cause.message : "Не удалось создать QR-код");
      }
    }
    void start();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [open, qrVersion]);

  function openLogin() {
    window.clearTimeout(closeTimer.current);
    closingRef.current = false; setClosing(false);
    if (!dialog.current?.open) dialog.current?.showModal();
    setOpen(true);
  }
  function closeLogin() {
    if (closingRef.current) return;
    closingRef.current = true; setClosing(true);
    setOpen(false);
    const delay = window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 180;
    closeTimer.current = window.setTimeout(() => { dialog.current?.close(); closingRef.current = false; setClosing(false); }, delay);
  }

  return <>
    <section className={`dashboard-secondary${authorized ? "" : " music-locked"}`} aria-label="Музыка">
      <div className={`music-background${adaptiveBackground && authorized && music?.cover && music.cover !== brokenCover ? " visible" : ""}`} aria-hidden="true">
        {music?.cover && <img src={music.cover} alt="" referrerPolicy="no-referrer" />}
      </div>
      <div className="music-content" inert={!authorized} aria-hidden={!authorized}>
        <div className="music-cover">{music?.cover && music.cover !== brokenCover && <img src={music.cover} alt={music.title ? `Обложка: ${music.title}` : "Обложка трека"} referrerPolicy="no-referrer" onError={() => setBrokenCover(music.cover)} />}</div>
        <div className="music-info">
          <div className="music-heading">
            <div className="music-labels">
              <div className="music-title" title={music?.title || "Ничего не играет"}>{music?.title || "Ничего не играет"}</div>
              <div className="music-artist-row">
                <div className="music-artist" title={music?.error || music?.artist || ""}>{music?.error || music?.artist || (authorized && !connected ? "Подключаемся к плееру…" : "Автор")}</div>
              </div>
            </div>
            <div className="music-controls">
              <button type="button" className="music-like" aria-label={liked ? "Убрать из понравившихся" : "Добавить в понравившиеся"} title={liked ? "Убрать из понравившихся" : "Добавить в понравившиеся"} aria-pressed={liked === true} disabled={!connected || liked === null || likePending} onClick={() => void toggleLike()}>{likePending ? <LoaderCircle className="auth-loading" size={16} /> : <Heart size={16} fill={liked ? "currentColor" : "none"} />}</button>
              <button type="button" aria-label="Предыдущий трек" title="Предыдущий трек" disabled={!connected || commandPending} onClick={() => void sendCommand("prev")}>{pendingCommand === "prev" ? <LoaderCircle className="auth-loading" size={16} /> : <SkipBack size={16} />}</button>
              <button ref={playButton} type="button" aria-label={music?.playing ? "Пауза" : "Воспроизвести"} title={music?.playing ? "Пауза" : "Воспроизвести"} disabled={!connected || commandPending} onClick={() => void sendCommand(music?.playing ? "stop" : "play")}>{pendingCommand === "play" || pendingCommand === "stop" ? <LoaderCircle className="auth-loading" size={16} /> : music?.playing ? <Pause size={16} /> : <Play size={16} />}</button>
              <button type="button" aria-label="Следующий трек" title="Следующий трек" disabled={!connected || commandPending} onClick={() => void sendCommand("next")}>{pendingCommand === "next" ? <LoaderCircle className="auth-loading" size={16} /> : <SkipForward size={16} />}</button>
            </div>
          </div>
          <div className="music-progress">
            <div className="music-seek-track">
              <svg viewBox="0 0 320 16" preserveAspectRatio="none" aria-hidden="true"><path d={wave} /></svg>
              <div className="music-progress-played" style={{ clipPath: `inset(0 ${100 - progressRatio * 100}% 0 0)` }}><svg viewBox="0 0 320 16" preserveAspectRatio="none" aria-hidden="true"><path d={wave} /></svg></div>
              <input type="range" min="0" max={music?.duration || 1} step="1" value={music?.duration ? Math.min(progress, music.duration) : 0} aria-label="Позиция воспроизведения" aria-valuetext={musicTime(progress)} disabled={!connected || !music?.duration || commandPending} onChange={event => setSeek(Number(event.target.value))} onPointerUp={() => void commitSeek()} onKeyUp={event => { if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown"].includes(event.key)) void commitSeek(); }} onBlur={() => void commitSeek()} />
            </div>
            <div className="music-timestamps"><span>{music?.title ? musicTime(progress) : "--:--"}</span><span>{music?.duration ? musicTime(music.duration) : "--:--"}</span></div>
          </div>
        </div>
      </div>
      <div className={`music-auth-overlay${authorized ? " authorized" : ""}`} inert={authorized} aria-hidden={authorized}>
        <p>{auth === "checking" ? "Проверяем авторизацию…" : auth === "error" ? "Не удалось проверить авторизацию" : "Для работы с музыкой, авторизуйтесь по QR"}</p>
        {auth === "error" ? <button className="auth-button" type="button" onClick={() => { setAuth("checking"); setCheckVersion(value => value + 1); }}><RefreshCw size={14} />Повторить</button> : <button ref={loginButton} className="auth-button" type="button" onClick={openLogin} disabled={auth === "checking"}><QrCode size={15} />Авторизоваться</button>}
      </div>
    </section>
    <dialog className={`auth-dialog${closing ? " closing" : ""}`} ref={dialog} aria-labelledby="auth-title" aria-describedby="auth-description" onCancel={event => { event.preventDefault(); closeLogin(); }} onClose={() => { setOpen(false); setClosing(false); closingRef.current = false; if (authorized) playButton.current?.focus(); else loginButton.current?.focus(); }} onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) closeLogin(); } }}>
      <div className="auth-dialog-heading"><h2 id="auth-title">Вход в Яндекс</h2><button className="auth-close" type="button" aria-label="Закрыть окно авторизации" onClick={closeLogin}><X size={19} /></button></div>
      <p id="auth-description">Отсканируйте QR-код телефоном, на котором выполнен вход в Яндекс, и подтвердите авторизацию.</p>
      <div className={`auth-qr${qrState === "expired" ? " expired" : ""}`}>
        {link ? <QRCodeSVG value={link} size={208} level="M" marginSize={2} title="QR-код входа в Яндекс" /> : qrState === "loading" ? <LoaderCircle className="auth-loading" size={28} aria-label="Создаём QR-код" /> : <QrCode size={44} aria-hidden="true" />}
      </div>
      <div className="auth-qr-status" role="status">{qrState === "loading" ? "Создаём QR-код…" : qrState === "waiting" ? "Ожидаем подтверждения на телефоне…" : qrState === "expired" ? "Срок действия QR-кода истёк" : error}</div>
      {(qrState === "expired" || qrState === "error") && <button className="auth-button auth-retry" type="button" onClick={() => setQrVersion(value => value + 1)}><RefreshCw size={14} />Новый QR-код</button>}
    </dialog>
    {musicError && <div className="error-toast" role="alert"><CircleAlert size={18} /><span>{musicError}</span><button type="button" className="toast-close" aria-label="Закрыть уведомление" onClick={() => setMusicError("")}><X size={16} /></button></div>}
  </>;
}
