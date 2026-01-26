package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"time"

	"github.com/aquilax/go-perlin"
)

func GenerateLakes(width, height, numLakes int, lakeSize float64) (image.Image, []image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var allLakePixels []image.Point

	if numLakes == 0 {
		return canvas, allLakePixels
	}

	p := perlin.NewPerlin(2, 2, 5, rand.New(rand.NewSource(time.Now().UnixNano())).Int63())
	blobSize := int(math.Sqrt(float64(width*height) * (lakeSize / 100.0)))

	for i := 0; i < numLakes; i++ {
		// Create a noise map for the lake
		noiseMap := image.NewGray(image.Rect(0, 0, blobSize*2, blobSize*2))
		for x := 0; x < blobSize*2; x++ {
			for y := 0; y < blobSize*2; y++ {
				noise := p.Noise2D(float64(x)/float64(blobSize), float64(y)/float64(blobSize))
				grayColor := uint8((noise + 1) * 127.5)
				noiseMap.SetGray(x, y, color.Gray{Y: grayColor})
			}
		}

		// Find the largest contiguous area in the noise map
		var largestLake []*image.Point
		visited := make([][]bool, blobSize*2)
		for i := range visited {
			visited[i] = make([]bool, blobSize*2)
		}

		for x := 0; x < blobSize*2; x++ {
			for y := 0; y < blobSize*2; y++ {
				if !visited[x][y] {
					c := noiseMap.At(x, y)
					r, _, _, _ := c.RGBA()
					if r < 32768 {
						var currentLake []*image.Point
						q := []*image.Point{{X: x, Y: y}}
						visited[x][y] = true

						for len(q) > 0 {
							p := q[0]
							q = q[1:]
							currentLake = append(currentLake, p)

							for dx := -1; dx <= 1; dx++ {
								for dy := -1; dy <= 1; dy++ {
									if dx == 0 && dy == 0 {
										continue
									}
									nx, ny := p.X+dx, p.Y+dy
									if nx >= 0 && nx < blobSize*2 && ny >= 0 && ny < blobSize*2 && !visited[nx][ny] {
										c := noiseMap.At(nx, ny)
										r, _, _, _ := c.RGBA()
										if r < 32768 {
											visited[nx][ny] = true
											q = append(q, &image.Point{X: nx, Y: ny})
										}
									}
								}
							}
						}
						if len(currentLake) > len(largestLake) {
							largestLake = currentLake
						}
					}
				}
			}
		}

		// Draw the largest lake on the canvas
		lakeX := rand.Intn(width)
		lakeY := rand.Intn(height)
		for _, p := range largestLake {
			nx, ny := lakeX+p.X-blobSize, lakeY+p.Y-blobSize
			if nx >= 0 && nx < width && ny >= 0 && ny < height {
				canvas.Set(nx, ny, color.RGBA{R: 0, G: 0, B: 255, A: 255})
				allLakePixels = append(allLakePixels, image.Point{X: nx, Y: ny})
			}
		}
	}

	return canvas, allLakePixels
}

func DarkenLakeAreas(heightmap image.Image, lakePixels []image.Point) image.Image {
	bounds := heightmap.Bounds()
	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)

	for _, p := range lakePixels {
		c := composite.At(p.X, p.Y)
		r, g, b, a := c.RGBA()
		// Darken by 15%
		r = uint32(float64(r) * 0.85)
		g = uint32(float64(g) * 0.85)
		b = uint32(float64(b) * 0.85)
		composite.Set(p.X, p.Y, color.RGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: uint16(a)})
	}

	return composite
}
