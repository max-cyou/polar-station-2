import { useEffect, useRef, useState } from "react";
import { Eraser, TerminalSquare, Unplug } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

type ConnectionState = "offline" | "connecting" | "connected";
export function StationTerminal() {
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const socket = useRef<WebSocket | null>(null);
  const [state, setState] = useState<ConnectionState>("offline");
  const [message, setMessage] = useState("");
  useEffect(() => {
    if (!host.current) return;
    const view = new Terminal({
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace', fontSize: 12,
      lineHeight: 1.35, cursorBlink: true, scrollback: 3000,
      theme: { background: "#191b1d", foreground: "#d4d7db", cursor: "#c6cbd1", selectionBackground: "#6b748a55", black: "#292c30", brightBlack: "#757d86", red: "#e18484", green: "#91bf98", yellow: "#d5bd87", blue: "#8baed5", magenta: "#b8a0d2", cyan: "#8cbfc4", white: "#d4d7db" },
    });
    const sizing = new FitAddon(); view.loadAddon(sizing); view.open(host.current);
    terminal.current = view; fit.current = sizing;
    const input = view.onData(data => { if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify({ type: "input", data })); });
    const resize = view.onResize(({ cols, rows }) => { if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(JSON.stringify({ type: "resize", cols, rows })); });
    const observer = new ResizeObserver(() => { if (host.current?.clientWidth && host.current.clientHeight) sizing.fit(); });
    observer.observe(host.current); sizing.fit();
    return () => {
      const previous = socket.current; socket.current = null; previous?.close();
      observer.disconnect(); input.dispose(); resize.dispose(); view.dispose(); terminal.current = null; fit.current = null;
    };
  }, []);
  function connect() {
    if (socket.current || !terminal.current) return;
    fit.current?.fit();
    const view = terminal.current;
    setState("connecting"); setMessage("");
    const address = new URL("/api/terminal", window.location.href);
    address.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    address.searchParams.set("cols", String(Math.max(2, Math.min(500, view.cols))));
    address.searchParams.set("rows", String(Math.max(2, Math.min(200, view.rows))));
    const connection = new WebSocket(address);
    connection.binaryType = "arraybuffer"; socket.current = connection;
    const timeout = window.setTimeout(() => { if (socket.current === connection && connection.readyState !== WebSocket.CLOSED) { setMessage("Колонка не ответила на подключение"); connection.close(); } }, 10000);
    connection.onmessage = event => {
      if (socket.current !== connection) return;
      if (event.data instanceof ArrayBuffer) { view.write(new Uint8Array(event.data)); return; }
      try {
        const data = JSON.parse(event.data);
        if (data.type === "ready") { window.clearTimeout(timeout); view.reset(); setState("connected"); view.focus(); fit.current?.fit(); }
        else if (data.type === "exit" || data.type === "error") { setMessage(data.message); connection.close(); }
      } catch { /* Terminal output is carried in binary frames. */ }
    };
    connection.onerror = () => { if (socket.current === connection) setMessage("Не удалось подключиться к терминалу"); };
    connection.onclose = () => {
      window.clearTimeout(timeout);
      if (socket.current === connection) { socket.current = null; setState("offline"); }
    };
  }
  function disconnect() {
    const previous = socket.current; socket.current = null; previous?.close();
    setState("offline"); setMessage("Сеанс завершён");
  }
  return <section className="dashboard-side-block station-terminal" aria-label="Терминал станции">
    <header className="terminal-heading"><h2>Терминал</h2><div className="terminal-actions">
      <button type="button" aria-label="Очистить терминал" title="Очистить вывод" disabled={state !== "connected"} onClick={() => { terminal.current?.clear(); terminal.current?.focus(); }}><Eraser size={15} /></button>
      {state !== "offline" && <button type="button" className="terminal-connect" onClick={disconnect}><Unplug size={14} />{state === "connecting" ? "Отменить" : "Отключить"}</button>}
    </div></header>
    <div className="terminal-body"><div className="terminal-viewport" ref={host} />{state !== "connected" && <div className="terminal-idle"><TerminalSquare size={28} strokeWidth={1.3} /><span>{state === "connecting" ? "Подключение к колонке…" : message || "Консоль станции"}</span>{state === "offline" && <button type="button" onClick={connect}>Подключить</button>}</div>}</div>
  </section>;
}
