package main

import "image"

// PixelMask stores per-pixel occupancy using one byte per pixel.
type PixelMask struct {
	Width  int
	Height int
	Data   []uint8
}

func NewPixelMask(width, height int) *PixelMask {
	if width <= 0 || height <= 0 {
		return &PixelMask{}
	}
	return &PixelMask{
		Width:  width,
		Height: height,
		Data:   make([]uint8, width*height),
	}
}

func (m *PixelMask) index(x, y int) int {
	return y*m.Width + x
}

func (m *PixelMask) InBounds(x, y int) bool {
	return m != nil && x >= 0 && y >= 0 && x < m.Width && y < m.Height
}

func (m *PixelMask) GetXY(x, y int) bool {
	if !m.InBounds(x, y) {
		return false
	}
	return m.Data[m.index(x, y)] != 0
}

func (m *PixelMask) SetXY(x, y int) {
	if m.InBounds(x, y) {
		m.Data[m.index(x, y)] = 1
	}
}

func (m *PixelMask) GetPoint(p image.Point) bool {
	return m.GetXY(p.X, p.Y)
}

func (m *PixelMask) SetPoint(p image.Point) {
	m.SetXY(p.X, p.Y)
}

func (m *PixelMask) Merge(other *PixelMask) {
	if m == nil || other == nil || m.Width != other.Width || m.Height != other.Height {
		return
	}
	for i := range m.Data {
		if other.Data[i] != 0 {
			m.Data[i] = 1
		}
	}
}

func (m *PixelMask) AddPoints(points []image.Point) {
	for _, p := range points {
		m.SetPoint(p)
	}
}

func (m *PixelMask) ToPoints() []image.Point {
	if m == nil {
		return nil
	}
	pts := make([]image.Point, 0)
	for y := 0; y < m.Height; y++ {
		row := y * m.Width
		for x := 0; x < m.Width; x++ {
			if m.Data[row+x] != 0 {
				pts = append(pts, image.Point{X: x, Y: y})
			}
		}
	}
	return pts
}

func BuildMaskFromLakes(width, height int, lakes [][]image.Point) *PixelMask {
	mask := NewPixelMask(width, height)
	for _, lake := range lakes {
		mask.AddPoints(lake)
	}
	return mask
}
