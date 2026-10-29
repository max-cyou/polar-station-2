import { useEffect, useState } from "react";
import { LoaderCircle } from "lucide-react";

type Track = { id: string; title: string; artist: string; cover: string };

const placeholders: Track[] = [
  { id: "placeholder-1", title: "Must be a cool track", artist: "Cool Artist", cover: "" },
  { id: "placeholder-2", title: "Track cooler than the last one", artist: "DJ Even Cooler", cover: "" },
  { id: "placeholder-3", title: "And another...", artist: "Some Legend", cover: "" },
];

export function MusicQueue({ trackId, switching }: { trackId: string; switching: boolean }) {
  const [tracks, setTracks] = useState<Track[]>([]);
  const [source, setSource] = useState("");
  const [status, setStatus] = useState<"loading" | "ready" | "signedOut" | "error">("loading");

  useEffect(() => {
    if (switching) return;
    setStatus("loading");
    const controller = new AbortController();
    let timer = 0;
    let mismatches = 0;
    async function update() {
      let retry = false;
      try {
        const response = await fetch("/api/music/queue", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(20_000)]), cache: "no-store" });
        if (response.status === 401) { setTracks([]); setSource(""); setStatus("signedOut"); return; }
        if (!response.ok) throw new Error();
        const data = await response.json();
        if (!Array.isArray(data.tracks)) throw new Error();
        if (trackId && data.trackId !== trackId) {
          if (++mismatches >= 5) throw new Error();
          retry = true;
          return;
        }
        mismatches = 0;
        if (!controller.signal.aborted) { setTracks(data.tracks); setSource(typeof data.source === "string" ? data.source : ""); setStatus("ready"); }
      } catch { if (!controller.signal.aborted) { setTracks([]); setSource(""); setStatus("error"); } }
      finally { if (!controller.signal.aborted) timer = window.setTimeout(update, retry ? 1000 : 15_000); }
    }
    void update();
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [trackId, switching]);

  const placeholder = tracks.length === 0;
  const actualTracks = tracks.slice(0, 3);
  const visibleTracks = [...actualTracks, ...placeholders.slice(actualTracks.length)];
  return <section className="dashboard-extra" aria-label="Следующие треки">
    <div className={`queue-content${status === "loading" || switching ? " loading" : ""}`}>
      <div className="queue-heading"><h2>Далее</h2>{source && <span title={source}>{source}</span>}</div>
      <div className="queue-list">{visibleTracks.map((track, index) => <div className={`queue-track${index >= actualTracks.length ? " placeholder" : ""}`} key={`${track.id}-${index}`}>
        <div className="queue-cover">{track.cover && <img src={track.cover} alt="" referrerPolicy="no-referrer" />}</div>
        <div className="queue-labels"><div className="queue-title" title={track.title}>{track.title}</div><div className="queue-artist" title={track.artist}>{track.artist || "Неизвестный исполнитель"}</div></div>
      </div>)}</div>
      {placeholder && status !== "loading" && <span className="queue-placeholder-label">{status === "signedOut" ? "Войдите в Яндекс Музыку" : status === "error" ? "Не удалось загрузить треки" : "Следующие треки недоступны"}</span>}
    </div>
    {(status === "loading" || switching) && <div className="queue-loading" role="status"><LoaderCircle className="auth-loading" size={19} /><span>Загрузка…</span></div>}
  </section>;
}
