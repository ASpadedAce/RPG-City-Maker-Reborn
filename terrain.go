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

	minTreeSizePercent  = 0.2
	maxTreeSizePercent  = 15.0
	treeSizePercentStep = 0.2
)

func clampTreeSizePercent(v float64) float64 {
	if v < minTreeSizePercent {
		return minTreeSizePercent
	}
	if v > maxTreeSizePercent {
		return maxTreeSizePercent
	}
	return v
}

func snapTreeSizePercent(v float64) float64 {
	v = clampTreeSizePercent(v)
	steps := math.Round((v - minTreeSizePercent) / treeSizePercentStep)
	return clampTreeSizePercent(minTreeSizePercent + steps*treeSizePercentStep)
}

func normalizeTreeSizePercentRange(minPercent, maxPercent float64) (float64, float64) {
	minPercent = snapTreeSizePercent(minPercent)
	maxPercent = snapTreeSizePercent(maxPercent)
	if minPercent > maxPercent {
		minPercent, maxPercent = maxPercent, minPercent
	}
	return minPercent, maxPercent
}

func getTreeSizeRangePixels(minPercent, maxPercent float64, width, height int) (float64, float64) {
	minPercent, maxPercent = normalizeTreeSizePercentRange(minPercent, maxPercent)
	avgDim := averageImageDimension(width, height)
	if avgDim < 1 {
		avgDim = 1
	}
	minPx := (minPercent / 100.0) * avgDim
	maxPx := (maxPercent / 100.0) * avgDim
	if minPx < 1 {
		minPx = 1
	}
	if maxPx < 1 {
		maxPx = 1
	}
	return minPx, maxPx
}

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

// DarkenLakeAreas darkens the heightmap where water exists.
func DarkenLakeAreas(heightmap image.Image, waterMask *PixelMask) image.Image {
	bounds := heightmap.Bounds()
	width := bounds.Dx()

	lakeMask := image.NewRGBA(bounds)
	black := color.RGBA{0, 0, 0, 255}
	if waterMask != nil {
		for y := 0; y < waterMask.Height; y++ {
			row := y * waterMask.Width
			for x := 0; x < waterMask.Width; x++ {
				if waterMask.Data[row+x] != 0 {
					lakeMask.Set(x, y, black)
				}
			}
		}
	}

	blurRadius := float64(width) * 0.05
	blurredLakeMask := imaging.Blur(lakeMask, blurRadius)

	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)
	draw.DrawMask(composite, bounds, blurredLakeMask, image.Point{}, image.NewUniform(color.Alpha{192}), image.Point{}, draw.Over)

	return composite
}

// FlattenRoadAreas smooths terrain under roads.
func FlattenRoadAreas(heightmap image.Image, roadMask *PixelMask) image.Image {
	bounds := heightmap.Bounds()
	width := bounds.Dx()

	roadGrayMask := image.NewGray(bounds)
	if roadMask != nil {
		for y := 0; y < roadMask.Height; y++ {
			row := y * roadMask.Width
			for x := 0; x < roadMask.Width; x++ {
				if roadMask.Data[row+x] != 0 {
					roadGrayMask.SetGray(x, y, color.Gray{Y: 255})
				}
			}
		}
	}

	blurRadius := float64(width) * 0.01
	blurredRoadMask := imaging.Blur(roadGrayMask, blurRadius)
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

// GenerateTrees places trees on the map based on coverage and noise.
func GenerateTrees(img *image.RGBA, waterMask, roadMask, buildingMask *PixelMask, minTreeSize, maxTreeSize, treeCoverage, treeClumpiness float64, seed int64) *PixelMask {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	minTreeSizePx, maxTreeSizePx := getTreeSizeRangePixels(minTreeSize, maxTreeSize, width, height)

	totalPixels := width * height
	if totalPixels <= 0 || treeCoverage <= 0 {
		return NewPixelMask(width, height)
	}
	targetTreePixels := int((float64(totalPixels) * treeCoverage) / 100.0)
	if treeCoverage >= 100 {
		targetTreePixels = totalPixels
	}
	if targetTreePixels < 1 {
		targetTreePixels = 1
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

	if waterMask == nil {
		waterMask = NewPixelMask(width, height)
	}
	if roadMask == nil {
		roadMask = NewPixelMask(width, height)
	}
	if buildingMask == nil {
		buildingMask = NewPixelMask(width, height)
	}

	randSrc := rand.New(rand.NewSource(seed))
	numClumpTrees := max(1, int(treeClumpiness))

	initialPoints := make([]image.Point, 0, numClumpTrees)
	for range numClumpTrees {
		for range 100 {
			p := image.Point{X: randSrc.Intn(width), Y: randSrc.Intn(height)}
			if treeNoiseMap.GrayAt(p.X, p.Y).Y >= threshold && !waterMask.GetPoint(p) && !roadMask.GetPoint(p) && !buildingMask.GetPoint(p) {
				initialPoints = append(initialPoints, p)
				break
			}
		}
	}
	if len(initialPoints) == 0 {
		for i := 0; i < 256; i++ {
			p := image.Point{X: randSrc.Intn(width), Y: randSrc.Intn(height)}
			if treeNoiseMap.GrayAt(p.X, p.Y).Y >= threshold && !waterMask.GetPoint(p) && !roadMask.GetPoint(p) && !buildingMask.GetPoint(p) {
				initialPoints = append(initialPoints, p)
				break
			}
		}
		if len(initialPoints) == 0 {
			return NewPixelMask(width, height)
		}
	}

	minRadius := minTreeSizePx
	allPoints := poissonDiscSampling(width, height, minRadius, 30, initialPoints, func(p image.Point) bool {
		return treeNoiseMap.GrayAt(p.X, p.Y).Y >= threshold && !waterMask.GetPoint(p) && !roadMask.GetPoint(p) && !buildingMask.GetPoint(p)
	}, seed)
	treeMask := NewPixelMask(width, height)
	if len(allPoints) == 0 {
		return treeMask
	}
	treePixelsPlaced := 0
	sizeRand := rand.New(rand.NewSource(seed + 17))
	done := false
	for _, p := range allPoints {
		size := minTreeSizePx + sizeRand.Float64()*(maxTreeSizePx-minTreeSizePx)
		if size <= 0 {
			continue
		}
		r := size / 2
		r2 := r * r
		for y := p.Y - int(r); y <= p.Y+int(r); y++ {
			for x := p.X - int(r); x <= p.X+int(r); x++ {
				pt := image.Point{X: x, Y: y}
				if !pt.In(img.Bounds()) || waterMask.GetPoint(pt) || roadMask.GetPoint(pt) || buildingMask.GetPoint(pt) {
					continue
				}
				dx := float64(x - p.X)
				dy := float64(y - p.Y)
				if dx*dx+dy*dy > r2 {
					continue
				}
				idx := y*width + x
				if treeMask.Data[idx] == 0 {
					treeMask.Data[idx] = 1
					treePixelsPlaced++
					if treePixelsPlaced >= targetTreePixels {
						done = true
						break
					}
				}
			}
			if done {
				break
			}
		}
		if done {
			break
		}
	}

	for y := 0; y < treeMask.Height; y++ {
		row := y * treeMask.Width
		for x := 0; x < treeMask.Width; x++ {
			if treeMask.Data[row+x] != 0 {
				img.Set(x, y, color.RGBA{R: 0, G: 100, B: 0, A: 255})
			}
		}
	}

	return treeMask
}

// poissonDiscSampling generates randomly distributed points with minimum radius separation
func poissonDiscSampling(width, height int, minRadius float64, k int, initialPoints []image.Point, isValid func(image.Point) bool, seed int64) []image.Point {
	randSrc := rand.New(rand.NewSource(seed))
	points := initialPoints
	activeList := make([]int, len(initialPoints))
	for i := range initialPoints {
		activeList[i] = i
	}

	cellSize := minRadius / math.Sqrt(2)
	gridWidth := int(math.Ceil(float64(width)/cellSize)) + 1
	gridHeight := int(math.Ceil(float64(height)/cellSize)) + 1
	grid := make([]int32, gridWidth*gridHeight)
	for i := range grid {
		grid[i] = -1
	}

	for i, p := range points {
		gridX, gridY := int(float64(p.X)/cellSize), int(float64(p.Y)/cellSize)
		grid[gridY*gridWidth+gridX] = int32(i)
	}

	for len(activeList) > 0 {
		listIndex := randSrc.Intn(len(activeList))
		p := points[activeList[listIndex]]
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
					if checkX >= 0 && checkX < gridWidth && checkY >= 0 && checkY < gridHeight {
						g := grid[checkY*gridWidth+checkX]
						if g < 0 {
							continue
						}
						existing := points[int(g)]
						dist := math.Sqrt(math.Pow(float64(existing.X-newPoint.X), 2) + math.Pow(float64(existing.Y-newPoint.Y), 2))
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
				newIdx := len(points) - 1
				activeList = append(activeList, newIdx)
				grid[gridY*gridWidth+gridX] = int32(newIdx)
				found = true
			}
		}
		if !found {
			activeList = append(activeList[:listIndex], activeList[listIndex+1:]...)
		}
	}
	return points
}
