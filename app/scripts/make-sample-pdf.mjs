// Writes public/samples/PNT-014-rev3.pdf: a synthetic, clearly labelled demo procedure for the viewer.
// Plain PDF 1.4 with Helvetica (WinAnsi); no dependencies. Run: node scripts/make-sample-pdf.mjs
import { mkdirSync, writeFileSync } from "node:fs";

const W = 595, H = 842; // A4 in points
const esc = (s) =>
  [...s].map((ch) => {
    const c = ch.charCodeAt(0);
    if (ch === "(" || ch === ")" || ch === "\\") return "\\" + ch;
    if (c > 126) return "\\" + c.toString(8).padStart(3, "0"); // WinAnsi matches Latin-1 for these
    return ch;
  }).join("");

const pages = [
  { title: "1 Objeto", lines: ["Este procedimiento describe el montaje del husillo en los centros de", "mecanizado CNC-07 y CNC-08 de la Línea 2."] },
  { title: "2 Alcance y referencias", lines: ["Aplica a Mantenimiento y Calidad. Referencias: PNT-005 (calibración),", "PNT-009 (verificación de par), DRW-CNC07-SP (plano del husillo)."] },
  { title: "3 Seguridad", lines: ["Antes de intervenir, aplicar PNT-021 (bloqueo y etiquetado).", "Usar guantes y gafas. Retirar refrigerante de la zona de trabajo."] },
  {
    title: "4 Montaje", lines: [
      "4.1 Limpiar el cono del husillo y comprobar que no presenta golpes.",
      "",
      "4.2 Tuerca del husillo",
      "Apretar la tuerca del husillo a 40 Nm en dos pasadas en cruz (20 Nm / 40 Nm)",
      "con la llave dinamométrica TW-03.",
      "",
      "4.3 Verificar el juego axial con reloj comparador (máx. 0,01 mm).",
    ],
  },
  { title: "5 Verificación", lines: ["Registrar el par aplicado y el juego axial en el formulario F-007.", "Repetir la verificación tras 8 horas de funcionamiento (PNT-009)."] },
  { title: "6 Registros", lines: ["F-007 Registro de intervención de mantenimiento."] },
  { title: "7 Responsabilidades", lines: ["Técnico de mantenimiento: ejecuta. Calidad: revisa y archiva."] },
  { title: "8 Historial de revisiones", lines: ["rev.1 2021-04-02 Edición inicial.", "rev.2 2023-06-02 Nuevo útil de montaje.", "rev.3 2025-03-12 Par de apriete actualizado a 40 Nm."] },
  { title: "9 Anexos", lines: ["Anexo A: secuencia de apriete en cruz (ver plano DRW-CNC07-SP)."] },
];

function pageStream(p, i) {
  const out = [];
  const text = (x, y, size, s, font = "F1") => out.push(`BT /${font} ${size} Tf ${x} ${y} Td (${esc(s)}) Tj ET`);
  // Title block (cajetín) at the top
  out.push("0.85 G 0.6 w 56 770 483 44 re S 56 792 m 539 792 l S 300 770 m 300 814 l S");
  text(64, 798, 12, "PNT-014  Montaje del husillo", "F2");
  text(64, 777, 9, "Calidad / Línea 2, CNC-07, CNC-08");
  text(308, 798, 9, "Revisión 3, Aprobado 12/03/2025");
  text(308, 777, 9, `Aprobado por J. García, Página ${i + 1} de ${pages.length}`);
  // Section
  text(72, 730, 14, p.title, "F2");
  let y = 700;
  for (const line of p.lines) {
    if (line) text(72, y, line.startsWith("4.2 ") ? 11 : 11, line, line.match(/^\d\.\d /) && line.length < 30 ? "F2" : "F1");
    y -= 18;
  }
  // Synthetic marker in the footer
  text(72, 40, 8, "DOCUMENTO SINTÉTICO DE DEMOSTRACIÓN, Planta Norte (ficticia), Morse Atlas");
  return out.join("\n");
}

const objs = [];
const add = (s) => { objs.push(s); return objs.length; };
const catalog = add("<< /Type /Catalog /Pages 2 0 R >>");
const pagesIdx = add(""); // placeholder
const f1 = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>");
const f2 = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>");
const kids = [];
pages.forEach((p, i) => {
  const body = pageStream(p, i);
  const content = add(`<< /Length ${Buffer.byteLength(body, "latin1")} >>\nstream\n${body}\nendstream`);
  kids.push(add(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${W} ${H}] /Resources << /Font << /F1 ${f1} 0 R /F2 ${f2} 0 R >> >> /Contents ${content} 0 R >>`));
});
objs[pagesIdx - 1] = `<< /Type /Pages /Kids [${kids.map((k) => `${k} 0 R`).join(" ")}] /Count ${kids.length} >>`;
const info = add("<< /Title (PNT-014 rev.3 Montaje del husillo - SYNTHETIC DEMO) /Producer (Morse Atlas sample generator) >>");

let pdf = "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n";
const offsets = [];
objs.forEach((o, i) => { offsets.push(Buffer.byteLength(pdf, "latin1")); pdf += `${i + 1} 0 obj\n${o}\nendobj\n`; });
const xref = Buffer.byteLength(pdf, "latin1");
pdf += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offsets.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}`;
pdf += `trailer\n<< /Size ${objs.length + 1} /Root ${catalog} 0 R /Info ${info} 0 R >>\nstartxref\n${xref}\n%%EOF\n`;

mkdirSync("public/samples", { recursive: true });
writeFileSync("public/samples/PNT-014-rev3.pdf", Buffer.from(pdf, "latin1"));
console.log("wrote public/samples/PNT-014-rev3.pdf", pages.length, "pages");
