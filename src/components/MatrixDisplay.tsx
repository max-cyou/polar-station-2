import { useEffect, useState } from "react";

const digits: Record<string, string[]> = {
  "0": ["01110", "11011", "11011", "11011", "11011", "11011", "01110"],
  "1": ["00110", "01110", "00110", "00110", "00110", "00110", "01111"],
  "2": ["01110", "11011", "00011", "00110", "01100", "11000", "11111"],
  "3": ["11110", "00011", "00011", "01110", "00011", "00011", "11110"],
  "4": ["00011", "00111", "01111", "11011", "11111", "00011", "00011"],
  "5": ["11111", "11000", "11000", "11110", "00011", "11011", "01110"],
  "6": ["00110", "01100", "11000", "11110", "11011", "11011", "01110"],
  "7": ["11111", "00011", "00110", "00110", "01100", "01100", "01100"],
  "8": ["01110", "11011", "11011", "01110", "11011", "11011", "01110"],
  "9": ["01110", "11011", "11011", "01111", "00011", "00110", "01100"],
};
const columns = 29;
const rows = 21;

export function MatrixDisplay({ active, musicPlaying = false }: { active: boolean; musicPlaying?: boolean }) {
  const [now, setNow] = useState(() => new Date());
  const [musicWindow, setMusicWindow] = useState(false);
  useEffect(() => {
    setMusicWindow(musicPlaying);
    if (!musicPlaying) return;
    const timer = window.setInterval(() => setMusicWindow(value => !value), 5000);
    return () => window.clearInterval(timer);
  }, [musicPlaying]);
  const showNotes = musicPlaying && musicWindow && !active;
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const time = `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
  let lit = new Set<string>();
  const point = (x: number, y: number) => lit.add(`${x},${y}`);
  const paint = (pattern: string[], x: number, y: number) => {
    pattern.forEach((row, dy) => [...row].forEach((pixel, dx) => {
      if (pixel === "1") point(x + dx, y + dy);
    }));
  };

  const clock = new Set<string>();
  lit = clock;
  const numbers = time.replace(":", "");
  [2, 8, 16, 22].forEach((x, i) => paint(digits[numbers[i]], x, 7));
  if (now.getSeconds() % 2 === 0) { point(14, 9); point(14, 11); }

  const face = new Set<string>();
  lit = face;
  {
    const eye = ["0011100", "0111110", "1100011", "1100011", "1100011", "0100010"];
    paint(eye, 5, 8);
    paint(eye, 17, 8);
  }
  const note = new Set<string>();
  lit = note;
  paint(["000110", "000111", "000101", "000100", "000100", "011100", "111100", "011000"], 0, 0);
  const pattern = (pixels: Set<string>) => [...pixels].map((pixel) => {
    const [x, y] = pixel.split(",").map(Number);
    return <circle key={pixel} cx={(x + 5) * 4 + 2} cy={(y + 3) * 4 + 2} r="1.2" className="matrix-dot lit" />;
  });

  return (
    <div className={`matrix-display${active ? " matrix-active" : ""}`}>
      <svg viewBox="0 0 156 124" role="img" aria-label={active ? "Мордочка: вызов Алисы" : showNotes ? "Играет музыка" : `Часы ${time}`}>
        {Array.from({ length: (columns + 10) * (rows + 10) }, (_, i) => {
          const column = i % (columns + 10);
          const row = Math.floor(i / (columns + 10));
          const x = column - 5;
          const y = row - 3;
          const sideFade = Math.pow(Math.min(1, Math.max(0, (20 - Math.abs(x - 14)) / 8)), 1.6);
          const bottomFade = Math.pow(Math.min(1, Math.max(0, (28 - y) / 15)), 1.5);
          const topFade = Math.min(1, Math.max(0, (y + 4) / 5));
          return <circle key={i} cx={column * 4 + 2} cy={row * 4 + 2} r="1.2" className="matrix-dot" style={{ opacity: .05 * sideFade * bottomFade * topFade }} />;
        })}
        <g className={`matrix-pattern matrix-clock${!active && !showNotes ? " visible" : ""}`} aria-hidden="true">{pattern(clock)}</g>
        <g className={`matrix-pattern matrix-face${active ? " visible" : ""}`} aria-hidden="true">{pattern(face)}</g>
        <g className={`matrix-pattern matrix-music${showNotes ? " visible" : ""}`} aria-hidden="true">
          {[{ x: 0, y: 36 }, { x: 42, y: 46 }, { x: 80, y: 30 }].map((position, i) => (
            <g key={i} transform={`translate(${position.x} ${position.y})`}>
              <g className="matrix-note" style={{ animationDelay: `${i * -1.1}s`, animationDuration: `${3.2 + i * .4}s` }}>{pattern(note)}</g>
            </g>
          ))}
        </g>
      </svg>
    </div>
  );
}
