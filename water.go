package main

import (
	"container/heap"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"sort"

	"github.com/ojrac/opensimplex-go"
)

// lakePixel represents a potential pixel to be added to a lake during growth
type lakePixel struct {
	point image.Point
	score float64
	index int // required for heap.Interface
}

type priorityQueue []*lakePixel

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].score > pq[j].score } // Max-heap
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*lakePixel)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

func GenerateLakes(width, height, numLakes int, lakeSizeLower, lakeSizeUpper float64, heightmap image.Image, seed int64) (image.Image, [][]image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	if numLakes <= 0 || lakeSizeLower <= 0 {
		return canvas, nil
	}

	var allLakes [][]image.Point
	randSrc := rand.New(rand.NewSource(seed))

	// 1. Divide the image into a grid
	gridDim := int(math.Ceil(math.Sqrt(float64(numLakes))))
	if gridDim == 0 {
		return canvas, nil
	}
	chunkWidth := width / gridDim
	chunkHeight := height / gridDim
	if chunkWidth == 0 || chunkHeight == 0 {
		return canvas, nil
	}

	// 2. Create a list of chunk indices and shuffle them to randomize lake placement
	chunkIndices := make([]int, gridDim*gridDim)
	for i := range chunkIndices {
		chunkIndices[i] = i
	}
	randSrc.Shuffle(len(chunkIndices), func(i, j int) {
		chunkIndices[i], chunkIndices[j] = chunkIndices[j], chunkIndices[i]
	})

	totalArea := float64(width * height)
	noiseGen := opensimplex.New(seed)

	// 3. Generate a lake in a subset of the chunks
	for i := 0; i < numLakes; i++ {
		if i >= len(chunkIndices) {
			break
		}

		var currentLake []image.Point

		// Each lake gets a random size within the defined range
		lakeSize := lakeSizeLower
		if lakeSizeUpper > lakeSizeLower {
			lakeSize = lakeSizeLower + randSrc.Float64()*(lakeSizeUpper-lakeSizeLower)
		}
		targetPixelsPerLake := int(math.Round(totalArea*(lakeSize/100.0))) / 2
		if targetPixelsPerLake <= 0 {
			targetPixelsPerLake = 1
		}

		chunkIndex := chunkIndices[i]
		chunkGridX := chunkIndex % gridDim
		chunkGridY := chunkIndex / gridDim

		chunkRect := image.Rect(
			chunkGridX*chunkWidth,
			chunkGridY*chunkHeight,
			(chunkGridX+1)*chunkWidth,
			(chunkGridY+1)*chunkHeight,
		)

		// Use the growth algorithm within the chunk
		pq := &priorityQueue{}
		heap.Init(pq)
		visited := make(map[image.Point]bool)

		// Start near the center of the chunk
		startPt := image.Point{
			X: chunkRect.Min.X + chunkWidth/2,
			Y: chunkRect.Min.Y + chunkHeight/2,
		}
		// just in case the center is out of bounds
		if !startPt.In(chunkRect) {
			continue
		}

		seedX := randSrc.Float64() * 10000.0
		seedY := randSrc.Float64() * 10000.0
		radius := math.Sqrt(float64(targetPixelsPerLake) / math.Pi)
		noiseFreq := 0.01 + (0.2 / (radius + 1.0))

		getScore := func(pt image.Point) float64 {
			dx, dy := pt.X-startPt.X, pt.Y-startPt.Y
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			noise := noiseGen.Eval2(seedX+float64(dx)*noiseFreq, seedY+float64(dy)*noiseFreq)
			distPenalty := math.Pow(dist/radius, 3.0)
			luma, _, _, _ := heightmap.At(pt.X, pt.Y).RGBA()
			heightmapVal := float64(luma) / 65535.0
			heightmapEffect := (0.5 - heightmapVal) * 1.5
			return noise - distPenalty + heightmapEffect
		}

		heap.Push(pq, &lakePixel{point: startPt, score: getScore(startPt)})
		visited[startPt] = true

		lakeCount := 0
		for pq.Len() > 0 && lakeCount < targetPixelsPerLake {
			current := heap.Pop(pq).(*lakePixel)

			// The pixel is valid, claim it.
			canvas.Set(current.point.X, current.point.Y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			currentLake = append(currentLake, current.point)
			lakeCount++

			// Add neighbors, constrained to the chunk rectangle
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					neighbor := image.Point{X: current.point.X + dx, Y: current.point.Y + dy}

					if !neighbor.In(chunkRect) || visited[neighbor] {
						continue
					}

					visited[neighbor] = true
					heap.Push(pq, &lakePixel{
						point: neighbor,
						score: getScore(neighbor),
					})
				}
			}
		}
		if len(currentLake) > 0 {
			allLakes = append(allLakes, currentLake)
		}
	}

	return canvas, allLakes
}

type River struct {
	Width      float64
	Start, End image.Point
	Points     []image.Point
}

func GenerateRivers(width, height, numRivers int, minWidth, maxWidth, curvyness float64, inputImage image.Image, lakes [][]image.Point, seed int64, heightmap image.Image) (image.Image, []image.Point) {
	if numRivers == 0 {
		return inputImage, nil
	}

	canvas, ok := inputImage.(*image.RGBA)
	if !ok {
		canvas = image.NewRGBA(inputImage.Bounds())
		draw.Draw(canvas, canvas.Bounds(), inputImage, image.Point{}, draw.Src)
	}

	var allRiverPixels []image.Point
	randSrc := rand.New(rand.NewSource(seed))
	avgDim := float64(width+height) / 2.0

	isWater := make(map[image.Point]bool)
	lakePixelMap := make(map[image.Point]int)
	for i, lake := range lakes {
		for _, p := range lake {
			isWater[p] = true
			lakePixelMap[p] = i
		}
	}

	rivers := make([]River, numRivers)
	for i := 0; i < numRivers; i++ {
		widthPercent := float64(i) / float64(numRivers-1)
		if numRivers == 1 {
			widthPercent = 0.5
		}
		rivers[i].Width = maxWidth - widthPercent*(maxWidth-minWidth)
	}

	sort.Slice(rivers, func(i, j int) bool {
		return rivers[i].Width > rivers[j].Width
	})

	numControlPoints := int(avgDim * 0.03)
	if numControlPoints < 60 {
		numControlPoints = 60
	}

	for i := range rivers {
		r := &rivers[i]

		startEdge := randSrc.Intn(4)
		endEdge := (startEdge + randSrc.Intn(3) + 1) % 4

		r.Start = getPointOnEdge(width, height, startEdge, randSrc)
		r.End = getPointOnEdge(width, height, endEdge, randSrc)

		path := calculatePath(r.Start, r.End, curvyness/100.0, avgDim, randSrc, numControlPoints)

		for _, p := range path {
			if isWater[p] {
				if lakeIndex, isLake := lakePixelMap[p]; isLake {
					// Intersection is with a lake, find its center
					lakeCenter := findCenter(lakes[lakeIndex])
					r.End = lakeCenter
				} else {
					// Intersection is with another river
					r.End = p
				}
				path = calculatePath(r.Start, r.End, curvyness/100.0, avgDim, randSrc, numControlPoints)
				break
			}
		}

		riverWidthPx := (r.Width / 100.0) * avgDim
		radius := riverWidthPx / 2.0

		for _, p := range path {
			// When drawing river pixels, add them to isWater to detect river-river intersections
			drawCircle(canvas, p, radius, color.RGBA{R: 0, G: 0, B: 255, A: 255}, &allRiverPixels, isWater, heightmap)
		}
		r.Points = path
	}

	return canvas, allRiverPixels
}

func findCenter(pixels []image.Point) image.Point {
	if len(pixels) == 0 {
		return image.Point{}
	}
	var sumX, sumY int
	for _, p := range pixels {
		sumX += p.X
		sumY += p.Y
	}
	return image.Point{
		X: sumX / len(pixels),
		Y: sumY / len(pixels),
	}
}

func getPointOnEdge(width, height, edge int, randSrc *rand.Rand) image.Point {
	switch edge {
	case 0: // Top
		return image.Point{X: randSrc.Intn(width), Y: 0}
	case 1: // Right
		return image.Point{X: width - 1, Y: randSrc.Intn(height)}
	case 2: // Bottom
		return image.Point{X: randSrc.Intn(width), Y: height - 1}
	default: // Left
		return image.Point{X: 0, Y: randSrc.Intn(height)}
	}
}
func drawCircle(img *image.RGBA, center image.Point, radius float64, c color.Color, pixels *[]image.Point, isWater map[image.Point]bool, heightmap image.Image) {
	bounds := img.Bounds()
	r2 := radius * radius
	innerRadius := radius * 0.875 // The inner 75% of the river is smooth
	innerR2 := innerRadius * innerRadius

	for y := int(math.Floor(float64(center.Y) - radius)); y <= int(math.Ceil(float64(center.Y)+radius)); y++ {
		for x := int(math.Floor(float64(center.X) - radius)); x <= int(math.Ceil(float64(center.X)+radius)); x++ {
			p := image.Point{X: x, Y: y}
			if !p.In(bounds) {
				continue
			}

			dx, dy := float64(x-center.X), float64(y-center.Y)
			dist2 := dx*dx + dy*dy

			if dist2 <= r2 {
				if !isWater[p] {
					// Roughen the outer 15% of the river
					if dist2 > innerR2 {
						luma, _, _, _ := heightmap.At(x, y).RGBA()
						// Normalize luma to 0-1 range
						heightmapVal := float64(luma) / 65535.0
						// Roughen the edges based on the heightmap
						if heightmapVal < 0.5 {
							continue
						}
					}

					img.Set(x, y, c)
					*pixels = append(*pixels, p)
					isWater[p] = true
				}
			}
		}
	}
}
