import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, Copy, Gauge, LoaderCircle, Pause, Play, RefreshCw, RotateCw, Search, Square, X, XCircle } from "lucide-react";

type Process = { pid: number; name: string; cpu: number | null; memory: number; state: string; started: string; nice: number; controllable: boolean; clonable: boolean; restartable: boolean };
type Action = "terminate" | "kill" | "pause" | "resume" | "clone" | "restart" | "priority";
const actionNames: Record<Action, string> = { terminate: "Завершить", kill: "Убить принудительно", pause: "Приостановить", resume: "Продолжить", clone: "Запустить копию", restart: "Перезапустить", priority: "Приоритет" };
type SortKey = "pid" | "name" | "cpu" | "memory";
function memory(bytes: number) { return bytes >= 1024 ** 2 ? `${(bytes / 1024 ** 2).toFixed(1)} МБ` : `${Math.round(bytes / 1024)} КБ`; }
export function Processes() {
  const [processes, setProcesses] = useState<Process[]>([]);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<{ key: SortKey; descending: boolean }>({ key: "cpu", descending: true });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [version, setVersion] = useState(0);
  const [selection, setSelection] = useState<{ pid: number; started: string } | null>(null);
  const selected = processes.find(process => process.pid === selection?.pid && process.started === selection?.started);
  const [action, setAction] = useState<Action | null>(null);
  const [target, setTarget] = useState<Process | null>(null);
  const [nice, setNice] = useState(0);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [notice, setNotice] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const operationBusy = useRef(false);
  useEffect(() => { if (action) { if (!dialog.current?.open) dialog.current?.showModal(); } else dialog.current?.close(); }, [action]);
  function openAction(next: Action) {
    if (!selected?.controllable || busy) return;
    setTarget(selected); setNice(selected.nice); setActionError(""); setAction(next);
  }
  async function execute() {
    if (!action || !target || operationBusy.current) return;
    operationBusy.current = true; setBusy(true); setActionError(""); setNotice("");
    try {
      const response = await fetch("/api/processes/action", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ pid: target.pid, started: target.started, action, ...(action === "priority" ? { nice } : {}) }), signal: AbortSignal.timeout(18000) });
      const data = await response.json(); if (!response.ok) throw new Error(data.error || "Не удалось выполнить действие");
      if (data.pid) setNotice(data.exited ? `Процесс PID ${data.pid} завершился` : `Запущен процесс PID ${data.pid}`);
      else setNotice(action === "priority" ? "Приоритет изменён" : `Сигнал ${action === "terminate" ? "SIGTERM" : action === "kill" ? "SIGKILL" : action === "pause" ? "SIGSTOP" : "SIGCONT"} отправлен`);
      setAction(null); setVersion(value => value + 1);
    } catch (cause) { setActionError(cause instanceof Error ? cause.message : "Не удалось выполнить действие"); }
    finally { operationBusy.current = false; setBusy(false); }
  }

  useEffect(() => {
    const controller = new AbortController(); let timer = 0;
    setLoading(true);
    async function update() {
      try {
        const response = await fetch("/api/processes", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(8000)]), cache: "no-store" });
        if (!response.ok) throw new Error();
        const data = await response.json(); if (!Array.isArray(data.processes)) throw new Error();
        if (!controller.signal.aborted) { setProcesses(data.processes); setError(""); }
      } catch { if (!controller.signal.aborted) setError("Не удалось получить процессы"); }
      finally { if (!controller.signal.aborted) { setLoading(false); timer = window.setTimeout(update, 2000); } }
    }
    void update(); return () => { controller.abort(); window.clearTimeout(timer); };
  }, [version]);
  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    return processes.filter(process => process.name.toLocaleLowerCase().includes(needle) || String(process.pid).includes(needle)).sort((a, b) => {
      const result = sort.key === "name" ? a.name.localeCompare(b.name) : (a[sort.key] ?? -1) - (b[sort.key] ?? -1);
      return (sort.descending ? -result : result) || a.pid - b.pid;
    });
  }, [processes, query, sort]);
  function sortBy(key: SortKey) { setSort(previous => ({ key, descending: previous.key === key ? !previous.descending : key === "cpu" || key === "memory" })); }
  return <section className="dashboard-side-block process-manager" aria-label="Процессы станции">
    <div className="process-heading"><h2>Процессы</h2><button type="button" aria-label="Обновить процессы" title="Обновить" disabled={loading || busy} onClick={() => setVersion(value => value + 1)}>{loading ? <LoaderCircle className="auth-loading" size={15} /> : <RefreshCw size={14} />}</button></div>
    <label className="process-search"><Search size={14} aria-hidden="true" /><input aria-label="Поиск процесса или PID" placeholder="Имя или PID" value={query} onChange={event => setQuery(event.target.value)} spellCheck={false} /></label>
    <div className="process-toolbar" aria-label="Управление процессом">
      <button type="button" title="Завершить · SIGTERM" aria-label="Завершить процесс" disabled={!selected?.controllable || busy} onClick={() => openAction("terminate")}><Square size={14} /></button>
      <button type="button" title="Убить · SIGKILL" aria-label="Убить процесс принудительно" disabled={!selected?.controllable || busy} onClick={() => openAction("kill")}><XCircle size={15} /></button>
      <span className="file-toolbar-separator" />
      <button type="button" title={selected?.state === "T" || selected?.state === "t" ? "Продолжить · SIGCONT" : "Приостановить · SIGSTOP"} aria-label="Пауза или продолжение процесса" disabled={!selected?.controllable || busy} onClick={() => openAction(selected?.state === "T" || selected?.state === "t" ? "resume" : "pause")}>{selected?.state === "T" || selected?.state === "t" ? <Play size={14} /> : <Pause size={14} />}</button>
      <button type="button" title={selected && !selected.restartable ? "Перезапуск выполняется штатным супервизором" : "Перезапустить"} aria-label="Перезапустить процесс" disabled={!selected?.restartable || busy} onClick={() => openAction("restart")}><RotateCw size={14} /></button>
      <button type="button" title="Запустить копию" aria-label="Запустить копию процесса" disabled={!selected?.clonable || busy} onClick={() => openAction("clone")}><Copy size={14} /></button>
      <button type="button" title="Приоритет CPU" aria-label="Изменить приоритет процесса" disabled={!selected?.controllable || busy} onClick={() => openAction("priority")}><Gauge size={15} /></button>
      {busy && <LoaderCircle className="auth-loading" size={14} />}
    </div>
    <div className="process-table" role="table" aria-label="Список процессов">
      <div className="process-columns" role="row">{([["pid", "PID"], ["name", "Название"], ["cpu", "CPU"], ["memory", "Память"]] as const).map(([key, label]) => <div role="columnheader" aria-sort={sort.key === key ? sort.descending ? "descending" : "ascending" : "none"} key={key}><button type="button" onClick={() => sortBy(key)}>{label}{sort.key === key && <ChevronDown size={10} style={{ transform: sort.descending ? undefined : "rotate(180deg)" }} />}</button></div>)}</div>
      <div className="process-list" role="rowgroup" aria-busy={loading}>
        {loading && !processes.length ? <div className="file-empty"><LoaderCircle className="auth-loading" size={18} /><span>Загрузка…</span></div> : error ? <div className="file-empty file-error" role="alert">{error}</div> : !filtered.length ? <div className="file-empty">Процессы не найдены</div> : filtered.map(process => <div className="process-row" role="row" aria-selected={selected?.pid === process.pid && selected.started === process.started} tabIndex={0} key={`${process.pid}-${process.started}`} onClick={() => { if (!busy) setSelection({ pid: process.pid, started: process.started }); }} onKeyDown={event => { if ((event.key === "Enter" || event.key === " ") && !busy) { event.preventDefault(); setSelection({ pid: process.pid, started: process.started }); } }} title={`${process.name}\nPID ${process.pid} · ${process.state}`}><span role="cell" className="process-pid">{process.pid}</span><span role="cell" className="process-name">{process.name}</span><span role="cell" className="process-cpu">{process.cpu == null ? "—" : `${process.cpu.toFixed(1)}%`}</span><span role="cell" className="process-memory">{memory(process.memory)}</span></div>)}
      </div>
    </div>
    {notice && <div className="process-notice" role="status"><span>{notice}</span><button type="button" aria-label="Закрыть сообщение" onClick={() => setNotice("")}><X size={12} /></button></div>}
    <div className="process-status"><span>{query ? `${filtered.length} из ${processes.length}` : `${processes.length} процессов`}</span><span>{selected ? `PID ${selected.pid} · nice ${selected.nice} · ${selected.state}` : "Обновление каждые 2 с"}</span></div>
    <dialog className="auth-dialog process-dialog" ref={dialog} aria-labelledby="process-action-title" onCancel={event => { if (busy) event.preventDefault(); }} onClose={() => setAction(null)}>
      <div className="auth-dialog-heading"><h2 id="process-action-title">{action ? actionNames[action] : "Процесс"}</h2><button type="button" className="auth-close" aria-label="Закрыть" disabled={busy} onClick={() => setAction(null)}><X size={18} /></button></div>
      <p className="process-target">{target?.name}<span>PID {target?.pid}</span></p>
      {action === "priority" ? <label className="process-priority"><span>Приоритет <output>{nice}</output></span><input type="range" min={-20} max={19} value={nice} disabled={busy} onChange={event => setNice(Number(event.target.value))} /><small>Меньше число — выше приоритет.</small></label> : <p>{action === "clone" ? "Запускается новый экземпляр с теми же аргументами и владельцем." : action === "restart" ? "Процесс завершится и запустится заново с теми же аргументами." : action === "kill" ? "Отправить SIGKILL? Процесс завершится без ожидания." : action === "terminate" ? "Отправить процессу сигнал завершения SIGTERM?" : action === "pause" ? "Приостановить выполнение процесса?" : "Продолжить выполнение процесса?"}</p>}
      {actionError && <p className="file-error" role="alert">{actionError}</p>}
      <div className="file-dialog-actions"><button type="button" className="auth-button" disabled={busy} onClick={() => setAction(null)}>Отмена</button><button type="button" className="auth-button" disabled={busy} onClick={() => void execute()}>{busy && <LoaderCircle className="auth-loading" size={14} />}{action ? actionNames[action] : "Применить"}</button></div>
    </dialog>
  </section>;
}
