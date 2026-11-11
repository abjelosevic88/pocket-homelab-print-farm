// A small WebGL view of a model (binary or ASCII STL): drag to turn it,
// scroll or pinch to zoom. No 3D library: one shader, the build plate as a
// grid, the model lit from above.
import { useCallback, useEffect, useRef, useState } from 'react'
import { Icon } from './icons'
import { Loading } from './ui'

type Mesh = { pos: Float32Array; nor: Float32Array; min: number[]; max: number[] }

function parseSTL(buf: ArrayBuffer): Mesh {
  const dv = new DataView(buf)
  let tris: number[][] = []
  const isBinary = buf.byteLength >= 84 && 84 + dv.getUint32(80, true) * 50 === buf.byteLength
  const pos: number[] = []
  if (isBinary) {
    const n = dv.getUint32(80, true)
    for (let i = 0; i < n; i++) {
      const o = 84 + i * 50 + 12
      for (let k = 0; k < 9; k++) pos.push(dv.getFloat32(o + k * 4, true))
    }
  } else {
    const text = new TextDecoder().decode(buf)
    const re = /vertex\s+([-+\d.eE]+)\s+([-+\d.eE]+)\s+([-+\d.eE]+)/g
    let m
    while ((m = re.exec(text))) pos.push(+m[1], +m[2], +m[3])
  }
  tris = []
  void tris
  const nor = new Float32Array(pos.length)
  const min = [Infinity, Infinity, Infinity], max = [-Infinity, -Infinity, -Infinity]
  for (let i = 0; i < pos.length; i += 9) {
    const ux = pos[i + 3] - pos[i], uy = pos[i + 4] - pos[i + 1], uz = pos[i + 5] - pos[i + 2]
    const vx = pos[i + 6] - pos[i], vy = pos[i + 7] - pos[i + 1], vz = pos[i + 8] - pos[i + 2]
    let nx = uy * vz - uz * vy, ny = uz * vx - ux * vz, nz = ux * vy - uy * vx
    const l = Math.hypot(nx, ny, nz) || 1
    nx /= l; ny /= l; nz /= l
    for (let k = 0; k < 3; k++) {
      nor[i + k * 3] = nx; nor[i + k * 3 + 1] = ny; nor[i + k * 3 + 2] = nz
      for (let a = 0; a < 3; a++) { const v = pos[i + k * 3 + a]; if (v < min[a]) min[a] = v; if (v > max[a]) max[a] = v }
    }
  }
  return { pos: new Float32Array(pos), nor, min, max }
}

// ---- tiny matrix helpers (column-major) ----
type M4 = Float32Array
const persp = (fovy: number, asp: number, near: number, far: number): M4 => {
  const f = 1 / Math.tan(fovy / 2), nf = 1 / (near - far)
  return new Float32Array([f / asp, 0, 0, 0, 0, f, 0, 0, 0, 0, (far + near) * nf, -1, 0, 0, 2 * far * near * nf, 0])
}
const mul = (a: M4, b: M4): M4 => {
  const o = new Float32Array(16)
  for (let i = 0; i < 4; i++) for (let j = 0; j < 4; j++) { let s = 0; for (let k = 0; k < 4; k++) s += a[k * 4 + j] * b[i * 4 + k]; o[i * 4 + j] = s }
  return o
}
const rotX = (a: number): M4 => { const c = Math.cos(a), s = Math.sin(a); return new Float32Array([1, 0, 0, 0, 0, c, s, 0, 0, -s, c, 0, 0, 0, 0, 1]) }
const rotZ = (a: number): M4 => { const c = Math.cos(a), s = Math.sin(a); return new Float32Array([c, s, 0, 0, -s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]) }
const trans = (x: number, y: number, z: number): M4 => new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, y, z, 1])

const VS = `attribute vec3 p; attribute vec3 n; uniform mat4 mvp; uniform mat4 model; varying vec3 vn; varying float vz;
void main(){ vn = (model * vec4(n,0.0)).xyz; vz = p.z; gl_Position = mvp * vec4(p,1.0); }`
const FS = `precision mediump float; varying vec3 vn; varying float vz; uniform vec3 color; uniform float lines;
void main(){ if (lines > 0.5) { gl_FragColor = vec4(color, 1.0); return; }
  vec3 n = normalize(vn); vec3 l = normalize(vec3(-0.4, 0.5, 0.8)); float d = abs(dot(n, l));
  float rim = pow(1.0 - abs(n.z), 2.0) * 0.08;
  gl_FragColor = vec4(color * (0.32 + 0.7 * d) + rim, 1.0); }`

export function Viewer3D({ url, color = '#e8641b', onSnapshot, bed }: { url: string; color?: string; onSnapshot?: (b: Blob) => void; bed?: [number, number] }) {
  const wrap = useRef<HTMLDivElement | null>(null)
  const canvas = useRef<HTMLCanvasElement | null>(null)
  const [mesh, setMesh] = useState<Mesh | null>(null)
  const [err, setErr] = useState('')
  const view = useRef({ yaw: -0.6, pitch: 1.0, zoom: 1 })
  const draw = useRef<() => void>(() => {})

  useEffect(() => {
    let alive = true
    setMesh(null); setErr('')
    fetch(url, { credentials: 'same-origin' }).then(async r => {
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || 'The shape could not be loaded')
      return r.arrayBuffer()
    }).then(b => { if (alive) { const m = parseSTL(b); if (!m.pos.length) throw new Error('There are no triangles in this file'); setMesh(m) } })
      .catch(e => alive && setErr((e as Error).message))
    return () => { alive = false }
  }, [url])

  const ref = useCallback((el: HTMLDivElement | null) => { wrap.current = el }, [])

  useEffect(() => {
    const cv = canvas.current, box = wrap.current
    if (!mesh || !cv || !box) return
    const gl = cv.getContext('webgl', { antialias: true, preserveDrawingBuffer: true, alpha: true })
    if (!gl) { setErr('This browser cannot draw 3D'); return }
    const sh = (type: number, src: string) => { const s = gl.createShader(type)!; gl.shaderSource(s, src); gl.compileShader(s); return s }
    const prog = gl.createProgram()!
    gl.attachShader(prog, sh(gl.VERTEX_SHADER, VS)); gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, FS)); gl.linkProgram(prog); gl.useProgram(prog)
    const buf = (data: Float32Array) => { const b = gl.createBuffer()!; gl.bindBuffer(gl.ARRAY_BUFFER, b); gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW); return b }
    const cx = (mesh.min[0] + mesh.max[0]) / 2, cy = (mesh.min[1] + mesh.max[1]) / 2, z0 = mesh.min[2]
    const pb = buf(mesh.pos), nb = buf(mesh.nor)
    // the plate: a grid around the model
    const span = Math.max(mesh.max[0] - mesh.min[0], mesh.max[1] - mesh.min[1], 20)
    const half = bed ? Math.max(bed[0], bed[1]) / 2 : span * 0.9
    const step = half > 120 ? 20 : half > 50 ? 10 : 5
    const grid: number[] = []
    for (let v = -Math.floor(half / step) * step; v <= half; v += step) grid.push(cx - half, cy + v, z0, cx + half, cy + v, z0, cx + v, cy - half, z0, cx + v, cy + half, z0)
    const gb = buf(new Float32Array(grid)), gn = buf(new Float32Array(grid.length))
    const lp = gl.getAttribLocation(prog, 'p'), ln = gl.getAttribLocation(prog, 'n')
    const uMvp = gl.getUniformLocation(prog, 'mvp'), uModel = gl.getUniformLocation(prog, 'model'), uColor = gl.getUniformLocation(prog, 'color'), uLines = gl.getUniformLocation(prog, 'lines')
    const rgb = [parseInt(color.slice(1, 3), 16) / 255, parseInt(color.slice(3, 5), 16) / 255, parseInt(color.slice(5, 7), 16) / 255]
    const dark = window.matchMedia('(prefers-color-scheme: dark)').matches || document.documentElement.dataset.theme === 'dark'
    const radius = Math.hypot(mesh.max[0] - mesh.min[0], mesh.max[1] - mesh.min[1], mesh.max[2] - mesh.min[2]) / 2 || 10
    const render = () => {
      const w = cv.width, h = cv.height
      gl.viewport(0, 0, w, h)
      gl.clearColor(0, 0, 0, 0)
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
      gl.enable(gl.DEPTH_TEST)
      const v = view.current
      const dist = radius * 2.6 / v.zoom
      const model = mul(rotX(-v.pitch), rotZ(v.yaw))
      const center = trans(-cx, -cy, -(z0 + (mesh.max[2] - z0) / 2))
      const mv = mul(trans(0, 0, -dist), mul(model, center))
      const mvp = mul(persp(0.7, w / Math.max(1, h), dist / 50, dist * 10), mv)
      gl.uniformMatrix4fv(uMvp, false, mvp)
      gl.uniformMatrix4fv(uModel, false, model)
      // grid
      gl.uniform1f(uLines, 1); gl.uniform3fv(uColor, dark ? [0.32, 0.33, 0.36] : [0.82, 0.83, 0.86])
      gl.bindBuffer(gl.ARRAY_BUFFER, gb); gl.vertexAttribPointer(lp, 3, gl.FLOAT, false, 0, 0); gl.enableVertexAttribArray(lp)
      gl.bindBuffer(gl.ARRAY_BUFFER, gn); gl.vertexAttribPointer(ln, 3, gl.FLOAT, false, 0, 0); gl.enableVertexAttribArray(ln)
      gl.drawArrays(gl.LINES, 0, grid.length / 3)
      // model
      gl.uniform1f(uLines, 0); gl.uniform3fv(uColor, rgb)
      gl.bindBuffer(gl.ARRAY_BUFFER, pb); gl.vertexAttribPointer(lp, 3, gl.FLOAT, false, 0, 0)
      gl.bindBuffer(gl.ARRAY_BUFFER, nb); gl.vertexAttribPointer(ln, 3, gl.FLOAT, false, 0, 0)
      gl.drawArrays(gl.TRIANGLES, 0, mesh.pos.length / 3)
    }
    let raf = 0
    draw.current = () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(render) }
    const size = () => {
      const r = box.getBoundingClientRect(), dpr = Math.min(2, window.devicePixelRatio || 1)
      cv.width = Math.max(1, Math.round(r.width * dpr)); cv.height = Math.max(1, Math.round(r.height * dpr))
      cv.style.width = `${Math.round(r.width)}px`; cv.style.height = `${Math.round(r.height)}px`
      draw.current()
    }
    const ro = new ResizeObserver(size)
    ro.observe(box)
    size()
    // turning and zooming
    const pts = new Map<number, { x: number; y: number }>()
    let pinch = 0
    const down = (e: PointerEvent) => { cv.setPointerCapture(e.pointerId); pts.set(e.pointerId, { x: e.clientX, y: e.clientY }) }
    const move = (e: PointerEvent) => {
      const p = pts.get(e.pointerId)
      if (!p) return
      if (pts.size === 2) {
        pts.set(e.pointerId, { x: e.clientX, y: e.clientY })
        const [a, b] = [...pts.values()]
        const d = Math.hypot(a.x - b.x, a.y - b.y)
        if (pinch) view.current.zoom = Math.max(0.3, Math.min(8, view.current.zoom * d / pinch))
        pinch = d
      } else {
        view.current.yaw += (e.clientX - p.x) * 0.01
        view.current.pitch = Math.max(0.05, Math.min(Math.PI - 0.05, view.current.pitch + (e.clientY - p.y) * 0.01))
        pts.set(e.pointerId, { x: e.clientX, y: e.clientY })
      }
      draw.current()
    }
    const up = (e: PointerEvent) => { pts.delete(e.pointerId); pinch = 0 }
    const wheel = (e: WheelEvent) => { e.preventDefault(); view.current.zoom = Math.max(0.3, Math.min(8, view.current.zoom * (e.deltaY < 0 ? 1.1 : 0.9))); draw.current() }
    cv.addEventListener('pointerdown', down); cv.addEventListener('pointermove', move); cv.addEventListener('pointerup', up); cv.addEventListener('pointercancel', up)
    cv.addEventListener('wheel', wheel, { passive: false })
    return () => {
      ro.disconnect(); cancelAnimationFrame(raf)
      cv.removeEventListener('pointerdown', down); cv.removeEventListener('pointermove', move); cv.removeEventListener('pointerup', up); cv.removeEventListener('pointercancel', up)
      cv.removeEventListener('wheel', wheel)
    }
  }, [mesh, color, bed])

  const reset = () => { view.current = { yaw: -0.6, pitch: 1.0, zoom: 1 }; draw.current() }
  const snap = () => { canvas.current?.toBlob(b => { if (b) onSnapshot?.(b) }, 'image/png') }

  return (
    <div className="viewer" ref={ref}>
      {err ? <div className="viewer-msg muted"><Icon name="package" size={28} />{err}</div> : !mesh ? <div className="viewer-msg"><Loading label="Drawing the model…" /></div> : null}
      <canvas ref={canvas} aria-label="The model in 3D: drag to turn it" style={{ visibility: mesh ? 'visible' : 'hidden' }} />
      {mesh && <div className="viewer-tools">
        <button type="button" className="btn sm ghost" onClick={reset} title="Turn back"><Icon name="rotate-ccw" size={14} /></button>
        {onSnapshot && <button type="button" className="btn sm ghost" onClick={snap} title="Use this view as the model's picture"><Icon name="camera" size={14} /></button>}
      </div>}
    </div>
  )
}
