package slicer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Render draws a mesh from the front, a little from above, as a shaded
// picture (PNG) with a transparent background: the model's picture until
// the slicer gives one.
func (m *Mesh) Render(size int, base color.RGBA) []byte {
	if len(m.Tris) == 0 || size <= 0 {
		return nil
	}
	// fit: centre and scale by the bounding sphere
	minX, minY, minZ := math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	maxX, maxY, maxZ := -math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64
	for _, t := range m.Tris {
		for i := 0; i < 9; i += 3 {
			minX, maxX = math.Min(minX, float64(t[i])), math.Max(maxX, float64(t[i]))
			minY, maxY = math.Min(minY, float64(t[i+1])), math.Max(maxY, float64(t[i+1]))
			minZ, maxZ = math.Min(minZ, float64(t[i+2])), math.Max(maxZ, float64(t[i+2]))
		}
	}
	cx, cy, cz := (minX+maxX)/2, (minY+maxY)/2, (minZ+maxZ)/2
	// camera: turned 35° around Z, looking down 25°
	yaw, pitch := 35*math.Pi/180, 25*math.Pi/180
	cyw, syw, cp, sp := math.Cos(yaw), math.Sin(yaw), math.Cos(pitch), math.Sin(pitch)
	view := func(x, y, z float64) (float64, float64, float64) {
		x, y, z = x-cx, y-cy, z-cz
		// around Z
		x, y = x*cyw-y*syw, x*syw+y*cyw
		// tilt: screen up is Z, depth is Y
		sy := z*cp + y*sp
		d := y*cp - z*sp
		return x, sy, d
	}
	sx0, sx1, sy0, sy1 := math.MaxFloat64, -math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64
	for _, t := range m.Tris {
		for i := 0; i < 9; i += 3 {
			x, y, _ := view(float64(t[i]), float64(t[i+1]), float64(t[i+2]))
			sx0, sx1, sy0, sy1 = math.Min(sx0, x), math.Max(sx1, x), math.Min(sy0, y), math.Max(sy1, y)
		}
	}
	span := math.Max(sx1-sx0, sy1-sy0)
	if span <= 0 {
		return nil
	}
	pad := float64(size) * 0.08
	scale := (float64(size) - 2*pad) / span
	ox := pad + ((float64(size)-2*pad)-(sx1-sx0)*scale)/2
	oy := pad + ((float64(size)-2*pad)-(sy1-sy0)*scale)/2
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	zbuf := make([]float64, size*size)
	for i := range zbuf {
		zbuf[i] = math.Inf(1)
	}
	// light from the upper left front
	lx, ly, lz := -0.45, 0.75, -0.5
	ll := math.Sqrt(lx*lx + ly*ly + lz*lz)
	lx, ly, lz = lx/ll, ly/ll, lz/ll
	for _, t := range m.Tris {
		var p [3][3]float64
		for k := 0; k < 3; k++ {
			x, y, d := view(float64(t[k*3]), float64(t[k*3+1]), float64(t[k*3+2]))
			p[k] = [3]float64{ox + (x-sx0)*scale, float64(size) - (oy + (y-sy0)*scale), d}
		}
		// normal in view space (from the view-space points)
		ux, uy, uz := p[1][0]-p[0][0], -(p[1][1] - p[0][1]), p[1][2]-p[0][2]
		vx, vy, vz := p[2][0]-p[0][0], -(p[2][1] - p[0][1]), p[2][2]-p[0][2]
		nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
		nl := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if nl == 0 {
			continue
		}
		nx, ny, nz = nx/nl, ny/nl, nz/nl
		if nz > 0 { // facing away: flip so both sides are lit (meshes with odd winding)
			nx, ny, nz = -nx, -ny, -nz
		}
		light := 0.35 + 0.65*math.Max(0, nx*lx+ny*ly+nz*lz)
		c := color.RGBA{shade(base.R, light), shade(base.G, light), shade(base.B, light), 255}
		fill(img, zbuf, size, p, c)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func shade(v uint8, f float64) uint8 {
	x := float64(v) * f
	if f > 0.9 {
		x += (255 - x) * (f - 0.9) * 2
	}
	return uint8(math.Max(0, math.Min(255, x)))
}

// fill rasterises one triangle with a depth test.
func fill(img *image.RGBA, zbuf []float64, size int, p [3][3]float64, c color.RGBA) {
	minx := int(math.Max(0, math.Floor(math.Min(p[0][0], math.Min(p[1][0], p[2][0])))))
	maxx := int(math.Min(float64(size-1), math.Ceil(math.Max(p[0][0], math.Max(p[1][0], p[2][0])))))
	miny := int(math.Max(0, math.Floor(math.Min(p[0][1], math.Min(p[1][1], p[2][1])))))
	maxy := int(math.Min(float64(size-1), math.Ceil(math.Max(p[0][1], math.Max(p[1][1], p[2][1])))))
	area := (p[1][0]-p[0][0])*(p[2][1]-p[0][1]) - (p[2][0]-p[0][0])*(p[1][1]-p[0][1])
	if area == 0 {
		return
	}
	for y := miny; y <= maxy; y++ {
		for x := minx; x <= maxx; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			w0 := ((p[1][0]-px)*(p[2][1]-py) - (p[2][0]-px)*(p[1][1]-py)) / area
			w1 := ((p[2][0]-px)*(p[0][1]-py) - (p[0][0]-px)*(p[2][1]-py)) / area
			w2 := 1 - w0 - w1
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			d := w0*p[0][2] + w1*p[1][2] + w2*p[2][2]
			i := y*size + x
			if d < zbuf[i] {
				zbuf[i] = d
				img.SetRGBA(x, y, c)
			}
		}
	}
}
