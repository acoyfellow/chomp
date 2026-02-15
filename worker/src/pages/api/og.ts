import type { APIRoute } from 'astro'
import { Resvg } from '@cf-wasm/resvg'

function escapeXml(text: string): string {
  return text
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

function wrapText(text: string, maxChars: number): string[] {
  const words = text.split(' ')
  const lines: string[] = []
  let current = ''
  for (const word of words) {
    const test = current ? `${current} ${word}` : word
    if (test.length <= maxChars) {
      current = test
    } else {
      if (current) lines.push(current)
      current = word
    }
  }
  if (current) lines.push(current)
  return lines
}

function generateSVG(title: string, description: string): string {
  const titleLines = wrapText(title, 28).slice(0, 3)
  const descLines = description ? wrapText(description, 55).slice(0, 2) : []

  const titleY = 240
  const titleText = titleLines
    .map((line, i) => `<text x="60" y="${titleY + i * 72}" font-family="sans-serif" font-size="56" font-weight="700" fill="#FFFFFF">${escapeXml(line)}</text>`)
    .join('\n    ')

  const descY = titleY + titleLines.length * 72 + 24
  const descText = descLines
    .map((line, i) => `<text x="60" y="${descY + i * 32}" font-family="sans-serif" font-size="22" fill="rgba(255,255,255,0.75)">${escapeXml(line)}</text>`)
    .join('\n    ')

  return `<svg width="1200" height="630" viewBox="0 0 1200 630" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1200" y2="630" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="#18181b"/>
      <stop offset="100%" stop-color="#09090b"/>
    </linearGradient>
  </defs>
  <rect width="1200" height="630" fill="url(#bg)"/>
  <!-- Gold accent bar -->
  <rect x="60" y="180" width="48" height="4" rx="2" fill="#c8a630"/>
  <!-- Logo -->
  <text x="60" y="100" font-family="sans-serif" font-size="28" font-weight="700" fill="#FFFFFF">chomp<tspan fill="#c8a630">.</tspan></text>
  <!-- Title -->
  ${titleText}
  <!-- Description -->
  ${descText}
  <!-- Footer -->
  <text x="60" y="590" font-family="sans-serif" font-size="16" fill="rgba(255,255,255,0.4)">chomp.coey.dev</text>
  <!-- Corner accent -->
  <rect x="1140" y="0" width="60" height="4" fill="#c8a630"/>
</svg>`
}

let fontCache: { bold: Uint8Array | null; regular: Uint8Array | null } = { bold: null, regular: null }

async function loadFonts(fetchFn: typeof fetch, origin: string, assets?: Fetcher) {
  if (fontCache.bold && fontCache.regular) return

  const boldPath = '/fonts/sora-bold.ttf'
  const regularPath = '/fonts/sora-regular.ttf'

  const fetchBold = assets
    ? assets.fetch(new URL(boldPath, origin).toString())
    : fetchFn(`${origin}${boldPath}`)
  const fetchRegular = assets
    ? assets.fetch(new URL(regularPath, origin).toString())
    : fetchFn(`${origin}${regularPath}`)

  const [boldRes, regularRes] = await Promise.all([fetchBold, fetchRegular])
  const [boldBuf, regularBuf] = await Promise.all([boldRes.arrayBuffer(), regularRes.arrayBuffer()])

  fontCache.bold = new Uint8Array(boldBuf)
  fontCache.regular = new Uint8Array(regularBuf)
}

export const GET: APIRoute = async ({ url, locals }) => {
  const title = url.searchParams.get('title') || 'chomp'
  const description = url.searchParams.get('description') || ''
  const format = url.searchParams.get('format')

  const svg = generateSVG(title, description)

  if (format === 'svg') {
    return new Response(svg, {
      headers: {
        'Content-Type': 'image/svg+xml',
        'Cache-Control': 'public, max-age=31536000, immutable',
      },
    })
  }

  const env = locals.runtime.env
  await loadFonts(fetch, url.origin, env.ASSETS)

  const resvg = new Resvg(svg, {
    font: {
      fontBuffers: [fontCache.bold!, fontCache.regular!],
      defaultFontFamily: 'sans-serif',
    },
    fitTo: { mode: 'width' as const, value: 1200 },
  })
  const png = resvg.render().asPng()

  return new Response(png as unknown as BodyInit, {
    headers: {
      'Content-Type': 'image/png',
      'Cache-Control': 'public, max-age=3600',
    },
  })
}
