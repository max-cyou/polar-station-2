import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, ArrowUp, Copy, Download, File, Folder, FolderOpen, LoaderCircle, MoveRight, RefreshCw, Shield, Trash2, Upload, X } from "lucide-react";

type Entry = { name: string; directory: boolean; link: boolean; size: number; mode: string; permissions: string; modified: number };
type Action = "delete" | "chmod" | "copy" | "move";
const join = (directory: string, name: string) => `${directory === "/" ? "" : directory}/${name}`;
const parent = (path: string) => path.replace(/\/+$/, "").replace(/\/[^/]*$/, "") || "/";
function fileSize(bytes: number) { if (bytes < 1024) return `${bytes} Б`; if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} КБ`; if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} МБ`; return `${(bytes / 1024 ** 3).toFixed(1)} ГБ`; }

export function FileManager() {
  const [history, setHistory] = useState(["/data"]);
  const [historyIndex, setHistoryIndex] = useState(0);
  const path = history[historyIndex];
  const [draftPath, setDraftPath] = useState(path);
  const [files, setFiles] = useState<Entry[]>([]);
  const [selected, setSelected] = useState<Entry | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState("");
  const [uploadError, setUploadError] = useState("");
  const [action, setAction] = useState<Action | null>(null);
  const [target, setTarget] = useState<Entry | null>(null);
  const [targetPath, setTargetPath] = useState("");
  const [destination, setDestination] = useState("");
  const [permissions, setPermissions] = useState("0644");
  const [actionError, setActionError] = useState("");
  const uploadInput = useRef<HTMLInputElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const operationBusy = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    setDraftPath(path); setLoading(true); setError(""); setSelected(null); setFiles([]);
    fetch(`/api/files?${new URLSearchParams({ path })}`, { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(12000)]), cache: "no-store" })
      .then(async response => { const data = await response.json(); if (!response.ok) throw new Error(data.error || "Не удалось открыть каталог"); return data; })
      .then(data => { if (!controller.signal.aborted) setFiles(data.files); })
      .catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Не удалось открыть каталог"); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [path, version]);
  useEffect(() => { if (action) { if (!dialog.current?.open) dialog.current?.showModal(); } else dialog.current?.close(); }, [action]);

  function navigate(next: string) {
    if (busy) return;
    if (!next.startsWith("/")) { setError("Укажите абсолютный путь, например /data/opt"); return; }
    const parts: string[] = [];
    next.split("/").forEach(part => { if (part === "..") parts.pop(); else if (part && part !== ".") parts.push(part); });
    const clean = `/${parts.join("/")}`;
    setDraftPath(clean);
    if (clean === path) { setVersion(value => value + 1); return; }
    setHistory(previous => [...previous.slice(0, historyIndex + 1), clean]); setHistoryIndex(historyIndex + 1);
  }
  function openAction(next: Action) {
    if (!selected || busy || loading) return;
    const full = join(path, selected.name);
    setTarget(selected); setTargetPath(full); setDestination(next === "copy" ? `${full}.copy` : full); setPermissions(selected.mode); setActionError(""); setAction(next);
  }
  async function execute() {
    if (!action || !target || operationBusy.current) return;
    operationBusy.current = true; setBusy(true); setActionError("");
    try {
      const response = await fetch("/api/files/action", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, path: targetPath, ...(action === "chmod" ? { mode: permissions } : {}), ...(["copy", "move"].includes(action) ? { destination } : {}) }) });
      const data = await response.json(); if (!response.ok) throw new Error(data.error || "Не удалось выполнить действие");
      setAction(null); setVersion(value => value + 1);
    } catch (cause) { setActionError(cause instanceof Error ? cause.message : "Не удалось выполнить действие"); }
    finally { operationBusy.current = false; setBusy(false); }
  }
  async function upload(list: FileList | null) {
    if (!list?.length || operationBusy.current) return;
    const directory = path;
    operationBusy.current = true; setBusy(true); setError(""); setUploadError("");
    try {
      for (const file of Array.from(list)) {
        const data = new FormData(); data.append("file", file);
        const response = await fetch(`/api/files/upload?${new URLSearchParams({ path: directory })}`, { method: "POST", body: data });
        const result = await response.json(); if (!response.ok) throw new Error(result.error || `Не удалось загрузить ${file.name}`);
      }
    } catch (cause) { setUploadError(cause instanceof Error ? cause.message : "Не удалось загрузить файлы"); }
    finally { operationBusy.current = false; setBusy(false); setVersion(value => value + 1); if (uploadInput.current) uploadInput.current.value = ""; }
  }
  function togglePermission(bit: number) { setPermissions(previous => ((parseInt(previous, 8) || 0) ^ bit).toString(8).padStart(4, "0")); }
  const unavailable = busy || loading;
  const actionsDisabled = unavailable || !selected;
  const title = action === "delete" ? "Удалить файл?" : action === "chmod" ? "Права доступа" : action === "copy" ? "Копировать" : "Переместить";
  return <section className="dashboard-side-block file-manager" aria-label="Файловая система">
    <div className="file-manager-heading"><h2>Файлы</h2>{busy && <LoaderCircle className="auth-loading" size={16} aria-label="Выполняется операция" />}</div>
    <div className="file-navigation">
      <button type="button" title="Назад" aria-label="Назад" disabled={busy || historyIndex === 0} onClick={() => setHistoryIndex(value => value - 1)}><ArrowLeft size={15} /></button>
      <button type="button" title="Вперёд" aria-label="Вперёд" disabled={busy || historyIndex === history.length - 1} onClick={() => setHistoryIndex(value => value + 1)}><ArrowRight size={15} /></button>
      <button type="button" title="На уровень выше" aria-label="На уровень выше" disabled={busy || path === "/"} onClick={() => navigate(parent(path))}><ArrowUp size={15} /></button>
      <form onSubmit={event => { event.preventDefault(); navigate(draftPath); }}><input aria-label="Путь к каталогу" value={draftPath} onChange={event => setDraftPath(event.target.value)} disabled={busy} spellCheck={false} autoComplete="off" /></form>
    </div>
    <div className="file-toolbar" aria-label="Управление файлами">
      <button type="button" title="Загрузить файлы" aria-label="Загрузить файлы" disabled={unavailable} onClick={() => uploadInput.current?.click()}><Upload size={15} /></button>
      <button type="button" title="Скачать" aria-label="Скачать" disabled={actionsDisabled} onClick={() => { if (selected) { const anchor = document.createElement("a"); anchor.href = `/api/files/download?${new URLSearchParams({ path: join(path, selected.name) })}`; anchor.download = selected.directory ? `${selected.name}.tar.gz` : selected.name; document.body.appendChild(anchor); anchor.click(); anchor.remove(); } }}><Download size={15} /></button>
      <span className="file-toolbar-separator" />
      <button type="button" title="Копировать" aria-label="Копировать" disabled={actionsDisabled} onClick={() => openAction("copy")}><Copy size={15} /></button>
      <button type="button" title="Переместить" aria-label="Переместить" disabled={actionsDisabled} onClick={() => openAction("move")}><MoveRight size={15} /></button>
      <button type="button" title="Права доступа" aria-label="Права доступа" disabled={actionsDisabled || selected?.link} onClick={() => openAction("chmod")}><Shield size={15} /></button>
      <button type="button" title="Удалить" aria-label="Удалить" disabled={actionsDisabled} onClick={() => openAction("delete")}><Trash2 size={15} /></button>
      <button type="button" className="file-refresh" title="Обновить" aria-label="Обновить список" disabled={unavailable} onClick={() => setVersion(value => value + 1)}><RefreshCw size={14} /></button>
    </div>
    <input type="file" ref={uploadInput} hidden multiple onChange={event => void upload(event.target.files)} />
    <div className="file-list" role="listbox" aria-label="Файлы каталога" aria-busy={loading}>
      {loading ? <div className="file-empty"><LoaderCircle className="auth-loading" size={18} /><span>Загрузка…</span></div> : error ? <div className="file-empty file-error" role="alert">{error}</div> : files.length === 0 ? <div className="file-empty"><FolderOpen size={22} /><span>Каталог пуст</span></div> : files.map(entry => <button type="button" role="option" aria-selected={selected?.name === entry.name} key={entry.name} disabled={busy} className="file-entry" title={`${entry.name}\n${entry.permissions}\n${new Date(entry.modified * 1000).toLocaleString("ru-RU")}`} onClick={() => setSelected(entry)} onDoubleClick={() => { if (entry.directory) navigate(join(path, entry.name)); }} onKeyDown={event => { if (event.key === "Enter" && entry.directory) { event.preventDefault(); navigate(join(path, entry.name)); } }}>
        {entry.directory ? <Folder size={15} /> : <File size={15} />}<span className="file-name">{entry.name}{entry.link && <small> ↗</small>}</span><span className="file-size">{entry.directory ? "—" : fileSize(entry.size)}</span>
      </button>)}
    </div>
    {uploadError && <div className="file-operation-error" role="alert"><span title={uploadError}>{uploadError}</span><button type="button" aria-label="Закрыть ошибку" onClick={() => setUploadError("")}><X size={12} /></button></div>}
    <div className="file-manager-status"><span>{selected ? selected.name : `${files.length} элементов`}</span><span>{selected?.permissions ?? ""}</span></div>
    <dialog className="auth-dialog file-dialog" ref={dialog} aria-labelledby="file-dialog-title" onCancel={event => { if (busy) event.preventDefault(); }} onClose={() => setAction(null)}>
      <div className="auth-dialog-heading"><h2 id="file-dialog-title">{action === "delete" && target?.directory ? "Удалить каталог?" : title}</h2><button type="button" className="auth-close" disabled={busy} aria-label="Закрыть" onClick={() => setAction(null)}><X size={18} /></button></div>
      <p className="file-dialog-path">{targetPath}</p>
      {action === "delete" && <p>{target?.directory ? "Каталог будет удалён вместе со всем содержимым." : "Файл будет удалён с колонки."}</p>}
      {(action === "copy" || action === "move") && <label className="file-dialog-field">Путь назначения<input value={destination} onChange={event => setDestination(event.target.value)} spellCheck={false} disabled={busy} /></label>}
      {action === "chmod" && <><label className="file-dialog-field">Права в восьмеричном формате<input value={permissions} onChange={event => setPermissions(event.target.value)} spellCheck={false} maxLength={4} disabled={busy} /></label><div className="file-permissions"><span /><span>Чтение</span><span>Запись</span><span>Запуск</span>{[["Владелец", 6], ["Группа", 3], ["Остальные", 0]].map(([label, shift]) => <div className="file-permissions-row" key={label}><span>{label}</span>{[4, 2, 1].map(bit => <input key={bit} type="checkbox" aria-label={`${label}: ${bit === 4 ? "чтение" : bit === 2 ? "запись" : "запуск"}`} checked={!!((parseInt(permissions, 8) || 0) & (bit << Number(shift)))} disabled={busy} onChange={() => togglePermission(bit << Number(shift))} />)}</div>)}</div></>}
      {actionError && <p className="file-error" role="alert">{actionError}</p>}
      <div className="file-dialog-actions"><button type="button" className="auth-button" disabled={busy} onClick={() => setAction(null)}>Отмена</button><button type="button" className="auth-button" disabled={busy || (action === "chmod" && !/^[0-7]{3,4}$/.test(permissions)) || ((action === "copy" || action === "move") && !destination.startsWith("/"))} onClick={() => void execute()}>{busy ? <LoaderCircle className="auth-loading" size={14} /> : null}{action === "delete" ? "Удалить" : action === "chmod" ? "Применить" : action === "copy" ? "Копировать" : "Переместить"}</button></div>
    </dialog>
  </section>;
}
