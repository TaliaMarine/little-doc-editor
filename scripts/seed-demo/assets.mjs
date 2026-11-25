// Generates the demo attachments in memory (no binary fixtures in git).
import { deflateSync, crc32 } from 'node:zlib'

function chunk (type, data) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body))
  return Buffer.concat([len, body, crc])
}

// A small "architecture diagram": three labelled-by-colour boxes joined by arrows
// on a soft background. Pure RGB pixels, so no image library is needed.
export function diagramPng (width = 560, height = 240) {
  const px = Buffer.alloc(width * height * 3)
  const set = (x, y, [r, g, b]) => {
    if (x < 0 || y < 0 || x >= width || y >= height) return
    const o = (y * width + x) * 3
    px[o] = r; px[o + 1] = g; px[o + 2] = b
  }
  const rect = (x0, y0, w, h, c) => {
    for (let y = y0; y < y0 + h; y++) for (let x = x0; x < x0 + w; x++) set(x, y, c)
  }
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) set(x, y, [236 + (y * 12) / height, 241 + (y * 8) / height, 250])
  }
  const boxes = [
    [30, [63, 114, 175]],
    [220, [17, 122, 101]],
    [410, [196, 106, 40]],
  ]
  for (const [x, c] of boxes) {
    rect(x - 3, 77, 126, 96, [255, 255, 255])
    rect(x, 80, 120, 90, c)
    rect(x + 14, 98, 92, 10, [255, 255, 255])
    rect(x + 14, 120, 66, 8, [255, 255, 255, ])
    rect(x + 14, 138, 80, 8, [255, 255, 255])
  }
  for (const [from, to] of [[154, 216], [344, 406]]) {
    rect(from, 123, to - from, 4, [60, 72, 88])
    for (let i = 0; i < 9; i++) rect(to - i, 125 - i, 1, 2 * i + 1, [60, 72, 88])
  }
  const raw = Buffer.alloc((width * 3 + 1) * height)
  for (let y = 0; y < height; y++) {
    px.copy(raw, y * (width * 3 + 1) + 1, y * width * 3, (y + 1) * width * 3)
  }
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(width, 0)
  ihdr.writeUInt32BE(height, 4)
  ihdr[8] = 8; ihdr[9] = 2
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

const esc = s => s.replaceAll('\\', '\\\\').replaceAll('(', '\\(').replaceAll(')', '\\)')

// A valid multi-page text PDF (Helvetica, A4). `pages` is an array of
// { title, lines[] }.
export function textPdf (pages) {
  const objs = []
  const add = body => { objs.push(body); return objs.length }
  const catalog = add('') // 1, filled below
  const pagesObj = add('') // 2
  const font = add('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>')
  const bold = add('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>')
  const kids = []
  for (const page of pages) {
    let s = `BT /F2 22 Tf 56 780 Td (${esc(page.title)}) Tj ET\n`
    let y = 740
    for (const line of page.lines) {
      s += `BT /F1 12 Tf 56 ${y} Td (${esc(line)}) Tj ET\n`
      y -= 20
    }
    const content = add(`<< /Length ${Buffer.byteLength(s)} >>\nstream\n${s}endstream`)
    kids.push(add(`<< /Type /Page /Parent ${pagesObj} 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 ${font} 0 R /F2 ${bold} 0 R >> >> /Contents ${content} 0 R >>`))
  }
  objs[catalog - 1] = `<< /Type /Catalog /Pages ${pagesObj} 0 R >>`
  objs[pagesObj - 1] = `<< /Type /Pages /Kids [${kids.map(k => `${k} 0 R`).join(' ')}] /Count ${kids.length} >>`
  let out = '%PDF-1.4\n'
  const offsets = []
  objs.forEach((body, i) => {
    offsets.push(Buffer.byteLength(out))
    out += `${i + 1} 0 obj\n${body}\nendobj\n`
  })
  const xref = Buffer.byteLength(out)
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`
  for (const o of offsets) out += `${String(o).padStart(10, '0')} 00000 n \n`
  out += `trailer\n<< /Size ${objs.length + 1} /Root ${catalog} 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Buffer.from(out, 'latin1')
}
