package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"runtime"
	"sync"

	"github.com/aquilax/go-perlin"
	"github.com/disintegration/imaging"
	"github.com/ojrac/opensimplex-go"
)

const (
	alpha = 2.
	beta  = 2.
	n     = 3
)

// GenerateHeightmap creates terrain elevation using Perlin noise
func GenerateHeightmap(width, height, octaves int, scale float64, seed int64) image.Image {
	p := perlin.NewPerlin(alpha, beta, n, seed)
	img := image.NewGray(image.Rect(0, 0, width, height))

	if scale == 0 {
		scale = 100.0
	}

	numGoroutines := runtime.NumCPU()
	var wg sync.WaitGroup
	rowsPerGoroutine := height / numGoroutines

	for i := 0; i < numGoroutines; i++ {
		startY := i * rowsPerGoroutine
		endY := startY + rowsPerGoroutine
		if i == numGoroutines-1 {
			endY = height
		}
		wg.Add(1)
		go func(startY, endY int) {
			defer wg.Done()
			for y := startY; y < endY; y++ {
				for x := 0; x < width; x++ {
					var noise float64
					frequency := 1.0
					amplitude := 1.0
					maxAmplitude := 0.0

					for j := 0; j < octaves; j++ {
						noise += p.Noise2D(float64(x)*frequency/scale, float64(y)*frequency/scale) * amplitude
						maxAmplitude += amplitude
						amplitude /= 2.0
						frequency *= 2.0
					}

					noise /= maxAmplitude
					grayColor := uint8((noise + 1) * 127.5)
					img.SetGray(x, y, color.Gray{Y: grayColor})
				}
			}
		}(startY, endY)
	}
	wg.Wait()

	return img
}

// ApplyRoughness adds visual roughness effect to the heightmap
func ApplyRoughness(heightmap image.Image, roughness float64) image.Image {
	bounds := heightmap.Bounds()
	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)

	alphaValue := 255 - uint8(roughness*2.55)
	overlay := image.NewUniform(color.RGBA{R: 128, G: 128, B: 128, A: alphaValue})
	draw.Draw(composite, bounds, overlay, image.Point{}, draw.Over)

	return composite
}

// DarkenLakeAreas darkens the heightmap where lakes exist
func DarkenLakeAreas(heightmap image.Image, lakePixels []image.Point) image.Image {
	bounds := heightmap.Bounds()
	width := bounds.Dx()

	lakeMask := image.NewRGBA(bounds)
	black := color.RGBA{0, 0, 0, 255}
	for _, p := range lakePixels {
		lakeMask.Set(p.X, p.Y, black)
	}

	blurRadius := float64(width) * 0.05
	blurredLakeMask := imaging.Blur(lakeMask, blurRadius)

	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)
	draw.DrawMask(composite, bounds, blurredLakeMask, image.Point{}, image.NewUniform(color.Alpha{192}), image.Point{}, draw.Over)

	return composite
}

// FlattenRoadAreas smooths terrain under roads
func FlattenRoadAreas(heightmap image.Image, roadPixels []image.Point) image.Image {
	bounds := heightmap.Bounds()
	width := bounds.Dx()

	roadMask := image.NewGray(bounds)
	for _, p := range roadPixels {
		roadMask.SetGray(p.X, p.Y, color.Gray{Y: 255})
	}

	blurRadius := float64(width) * 0.01
	blurredRoadMask := imaging.Blur(roadMask, blurRadius)

	blurredHeightmap := imaging.Blur(heightmap, blurRadius)

	composite := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			maskAlpha, _, _, _ := blurredRoadMask.At(x, y).RGBA()
			if maskAlpha > 0 {
				originalColor := heightmap.At(x, y)
				blurredColor := blurredHeightmap.At(x, y)

				r1, g1, b1, a1 := originalColor.RGBA()
				r2, g2, b2, a2 := blurredColor.RGBA()

				alpha := float64(maskAlpha) / 65535.0

				r := uint16(float64(r1)*(1-alpha) + float64(r2)*alpha)
				g := uint16(float64(g1)*(1-alpha) + float64(g2)*alpha)
				b := uint16(float64(b1)*(1-alpha) + float64(b2)*alpha)
				a := uint16(float64(a1)*(1-alpha) + float64(a2)*alpha)

				composite.Set(x, y, color.RGBA64{R: r, G: g, B: b, A: a})
			} else {
				composite.Set(x, y, heightmap.At(x, y))
			}
		}
	}

	return composite
}

// GenerateTrees places trees on the map based on coverage and noise
func GenerateTrees(img *image.RGBA, lakePixels, roadPixels, buildingPixels []image.Point, minTreeSize, maxTreeSize, treeCoverage, treeClumpiness float64, seed int64) []image.Point {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	avgTreeSize := (minTreeSize + maxTreeSize) / 2
	if avgTreeSize <= 0 {
		return nil
	}
	avgRadius := avgTreeSize / 2
	avgTreeArea := math.Pi * avgRadius * avgRadius
	if avgTreeArea == 0 {
		return nil
	}
	totalArea := float64(width * height)
	targetTreePixels := totalArea * (treeCoverage / 100.0)
	numTreesToPlace := int(targetTreePixels / avgTreeArea)
	if numTreesToPlace == 0 {
		return nil
	}

	noise := opensimplex.New(seed)
	treeNoiseMap := image.NewGray(image.Rect(0, 0, width, height))
	treeNoiseZoom := 0.05
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			val := noise.Eval2(float64(x)*treeNoiseZoom, float64(y)*treeNoiseZoom)
			val = (val + 1) / 2
			treeNoiseMap.SetGray(x, y, color.Gray{Y: uint8(val * 255)})
		}
	}
	threshold := uint8(255 * (1 - (treeCoverage / 100.0)))

	isLake := make(map[image.Point]bool)
	for _, p := range lakePixels {
		isLake[p] = true
	}

	isRoad := make(map[image.Point]bool)
	for _, p := range roadPixels {
		isRoad[p] = true
	}

	isBuilding := make(map[image.Point]bool)
	for _, p := range buildingPixels {
		isBuilding[p] = true
	}

	randSrc := rand.New(rand.NewSource(seed))

	numClumpTrees := min(int(treeClumpiness), numTreesToPlace)

	initialPoints := make([]image.Point, 0, numClumpTrees)
	for range numClumpTrees {
		for range 100 {
			p := image.Point{X: randSrc.Intn(width), Y: randSrc.Intn(height)}
			if treeNoiseMap.GrayAt(p.X, p.Y).Y >= threshold && !isLake[p] && !isRoad[p] && !isBuilding[p] {
				initialPoints = append(initialPoints, p)
				break
			}
		}
	}

	minRadius := minTreeSize
	allPoints := poissonDiscSampling(width, height, minRadius, 30, initialPoints, func(p image.Point) bool {
		return treeNoiseMap.GrayAt(p.X, p.Y).Y >= threshold && !isLake[p] && !isRoad[p] && !isBuilding[p]
	}, seed)

	var treePixels []image.Point
	numGoroutines := runtime.NumCPU()
	if len(allPoints) < numGoroutines {
		numGoroutines = len(allPoints)
	}
	if numGoroutines == 0 {
		return nil
	}

	var wg sync.WaitGroup
	results := make(chan []image.Point, numGoroutines)
	pointsPerGoroutine := (len(allPoints) + numGoroutines - 1) / numGoroutines

	for i := 0; i < numGoroutines; i++ {
		start := i * pointsPerGoroutine
		if start >= len(allPoints) {
			break
		}
		end := start + pointsPerGoroutine
		if end > len(allPoints) {
			end = len(allPoints)
		}

		wg.Add(1)
		go func(points []image.Point, seed int64) {
			defer wg.Done()
			localRand := rand.New(rand.NewSource(seed))
			localTreePixels := make([]image.Point, 0)
			for _, p := range points {
				size := minTreeSize + localRand.Float64()*(maxTreeSize-minTreeSize)
				if size <= 0 {
					continue
				}
				r := size / 2
				for y := p.Y - int(r); y <= p.Y+int(r); y++ {
					for x := p.X - int(r); x <= p.X+int(r); x++ {
						pt := image.Point{X: x, Y: y}
						if !pt.In(img.Bounds()) || isLake[pt] || isRoad[pt] || isBuilding[pt] {
							continue
						}

						if (math.Pow(float64(x-p.X), 2) + math.Pow(float64(y-p.Y), 2)) <= r*r {
							img.Set(x, y, color.RGBA{R: 0, G: 100, B: 0, A: 255})
							localTreePixels = append(localTreePixels, pt)
						}
					}
				}
			}
			results <- localTreePixels
		}(allPoints[start:end], seed+int64(i))
	}

	wg.Wait()
	close(results)

	for res := range results {
		treePixels = append(treePixels, res...)
	}

	return treePixels
}

// poissonDiscSampling generates randomly distributed points with minimum radius separation
func poissonDiscSampling(width, height int, minRadius float64, k int, initialPoints []image.Point, isValid func(image.Point) bool, seed int64) []image.Point {
	randSrc := rand.New(rand.NewSource(seed))
	points := initialPoints
	activeList := append([]image.Point(nil), initialPoints...)

	cellSize := minRadius / math.Sqrt(2)
	gridWidth := int(math.Ceil(float64(width)/cellSize)) + 1
	gridHeight := int(math.Ceil(float64(height)/cellSize)) + 1
	grid := make([][]image.Point, gridWidth)
	for i := range grid {
		grid[i] = make([]image.Point, gridHeight)
	}

	for _, p := range points {
		gridX, gridY := int(float64(p.X)/cellSize), int(float64(p.Y)/cellSize)
		grid[gridX][gridY] = p
	}

	for len(activeList) > 0 {
		listIndex := randSrc.Intn(len(activeList))
		p := activeList[listIndex]
		found := false
		for range k {
			angle := randSrc.Float64() * 2 * math.Pi
			radius := minRadius + randSrc.Float64()*minRadius
			x, y := float64(p.X)+radius*math.Cos(angle), float64(p.Y)+radius*math.Sin(angle)
			newPoint := image.Point{X: int(x), Y: int(y)}

			if newPoint.X < 0 || newPoint.X >= width || newPoint.Y < 0 || newPoint.Y >= height {
				continue
			}

			if !isValid(newPoint) {
				continue
			}

			gridX, gridY := int(x/cellSize), int(y/cellSize)
			valid := true
			for m := -1; m <= 1; m++ {
				for n := -1; n <= 1; n++ {
					checkX, checkY := gridX+m, gridY+n
					if checkX >= 0 && checkX < gridWidth && checkY >= 0 && checkY < gridHeight && grid[checkX][checkY] != (image.Point{}) {
						dist := math.Sqrt(math.Pow(float64(grid[checkX][checkY].X-newPoint.X), 2) + math.Pow(float64(grid[checkX][checkY].Y-newPoint.Y), 2))
						if dist < minRadius {
							valid = false
							break
						}
					}
				}
				if !valid {
					break
				}
			}

			if valid {
				points = append(points, newPoint)
				activeList = append(activeList, newPoint)
				grid[gridX][gridY] = newPoint
				found = true
			}
		}
		if !found {
			activeList = append(activeList[:listIndex], activeList[listIndex+1:]...)
		}
	}
	return points
}
