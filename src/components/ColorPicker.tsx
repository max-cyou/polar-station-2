import { useEffect, useRef, useState } from "react";
import { Check, X } from "lucide-react";

const presets = ["#ffffff", "#a45cf5", "#5865f2", "#3584e4", "#26c6b5", "#45d778", "#f2cd45", "#ff974d", "#f05252", "#e01b24", "#f078bd", "#b8bec5"];

export function ColorPicker({ value, label, onChange }: { value: string; label: string; onChange: (color: string) => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [hex, setHex] = useState(value);
  useEffect(() => setHex(value), [value]);
  const rgb = [1, 3, 5].map(offset => parseInt(value.slice(offset, offset + 2), 16));
  function changeChannel(index: number, channel: number) {
    const next = [...rgb]; next[index] = channel;
    onChange("#" + next.map(n => n.toString(16).padStart(2, "0")).join(""));
  }
  return <>
    <button type="button" className="color-picker-trigger" aria-label={`${label}: ${value}`} aria-haspopup="dialog" onClick={() => dialog.current?.showModal()}>
      <span>{value.toUpperCase()}</span>
    </button>
    <dialog className="auth-dialog color-picker-dialog" ref={dialog} aria-label={label} onClick={event => {
      if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) dialog.current?.close(); }
    }}>
      <div className="auth-dialog-heading"><h2>{label}</h2><button type="button" className="auth-close" aria-label="Закрыть выбор цвета" onClick={() => dialog.current?.close()}><X size={18} /></button></div>
      <div className="color-picker-presets">{presets.map(color => <button key={color} type="button" style={{ background: color }} aria-label={color} aria-pressed={value.toLowerCase() === color} onClick={() => onChange(color)}>{value.toLowerCase() === color && <Check size={13} color={['#ffffff', '#f2cd45', '#b8bec5', '#45d778'].includes(color) ? '#202225' : '#fff'} />}</button>)}</div>
      <label className="color-picker-hex"><span>HEX</span><input aria-label="HEX-код цвета" value={hex.toUpperCase()} spellCheck={false} maxLength={7} onChange={event => { const next = event.target.value; setHex(next); if (/^#[0-9a-f]{6}$/i.test(next)) onChange(next.toLowerCase()); }} onBlur={() => setHex(value)} /></label>
      <div className="color-picker-channels">{["R", "G", "B"].map((name, i) => {
        const from = [...rgb]; from[i] = 0; const to = [...rgb]; to[i] = 255;
        return <label key={name}><span>{name}</span><input aria-label={`Канал ${name}`} type="range" min={0} max={255} value={rgb[i]} style={{ background: `linear-gradient(to right, rgb(${from.join(',')}), rgb(${to.join(',')}))` }} onChange={event => changeChannel(i, Number(event.target.value))} /><output>{rgb[i]}</output></label>;
      })}</div>
      <button className="auth-button color-picker-done" type="button" onClick={() => dialog.current?.close()}>Готово</button>
    </dialog>
  </>;
}
