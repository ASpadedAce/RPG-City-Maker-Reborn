package main

import (
	"image"
	"image/color"
	"math"
)

// GenerateBumpMap creates a bump map from a heightmap.
func GenerateBumpMap(heightmap *image.RGBA, width, height int, depth float64) *image.RGBA {
	bumpMap := image.NewRGBA(image.Rect(0, 0, width, height))

	sobelX := [][]int{
		{-1, 0, 1},
		{-2, 0, 2},
		{-1, 0, 1},
	}

	sobelY := [][]int{
		{-1, -2, -1},
		{0, 0, 0},
		{1, 2, 1},
	}

	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			var gx, gy float64

			for i := 0; i < 3; i++ {
				for j := 0; j < 3; j++ {
					gray, _, _, _ := heightmap.At(x+i-1, y+j-1).RGBA()
					intensity := float64(gray >> 8)
					gx += intensity * float64(sobelX[j][i])
					gy += intensity * float64(sobelY[j][i])
				}
			}

			nx := gx
			ny := gy
			nz := 1.0 / depth

			length := math.Sqrt(nx*nx + ny*ny + nz*nz)
			if length > 0 {
				nx /= length
				ny /= length
				nz /= length
			}

			r := uint8((nx + 1.0) * 127.5)
			g := uint8((ny + 1.0) * 127.5)
			b := uint8(nz * 255.0)

			bumpMap.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return bumpMap
}
