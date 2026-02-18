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

type lakePixel struct {
	point image.Point
	score float64
	index int
}

type priorityQueue []*lakePixel

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].score > pq[j].score }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*lakePixel)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// GenerateLakes creates lakes on the map using a priority queue growth algorithm
func GenerateLakes(width, height, numLakes int, lakeSizeLower, lakeSizeUpper float64, seed int64, lakeEdgeRoughness float64) (image.Image, [][]image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	if numLakes <= 0 || lakeSizeLower <= 0 {
		return canvas, nil
	}

	var allLakes [][]image.Point
	randSrc := rand.New(rand.NewSource(seed))

	// Divide the image into a grid to distribute lakes evenly
	gridDim := int(math.Ceil(math.Sqrt(float64(numLakes))))
	if gridDim == 0 {
		return canvas, nil
	}
	chunkWidth := width / gridDim
	chunkHeight := height / gridDim
	if chunkWidth == 0 || chunkHeight == 0 {
		return canvas, nil
	}

	// Shuffle chunk indices for random lake placement
	chunkIndices := make([]int, gridDim*gridDim)
	for i := range chunkIndices {
		chunkIndices[i] = i
	}
	randSrc.Shuffle(len(chunkIndices), func(i, j int) {
		chunkIndices[i], chunkIndices[j] = chunkIndices[j], chunkIndices[i]
	})

	totalArea := float64(width * height)
	noiseGen := opensimplex.New(seed)

	// Generate each lake
	for i := range numLakes {
		if i >= len(chunkIndices) {
			break
		}

		var currentLake []image.Point

		// Randomize lake size within specified range
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

		// Initialize priority queue growth algorithm
		pq := &priorityQueue{}
		heap.Init(pq)
		visited := make(map[image.Point]bool)

		// Start growth at chunk center
		startPt := image.Point{
			X: chunkRect.Min.X + chunkWidth/2,
			Y: chunkRect.Min.Y + chunkHeight/2,
		}
		if !startPt.In(chunkRect) {
			continue
		}

		// Setup noise generation for natural lake shapes
		seedX := randSrc.Float64() * 10000.0
		seedY := randSrc.Float64() * 10000.0
		radius := math.Sqrt(float64(targetPixelsPerLake) / math.Pi)
		noiseFreq := 0.01 + (0.2 / (radius + 1.0))

		// Score function determines which pixels to add to lake
		getScore := func(pt image.Point) float64 {
			dx, dy := pt.X-startPt.X, pt.Y-startPt.Y
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			distPenalty := math.Pow(dist/radius, 3.0)

			if lakeEdgeRoughness > 0 {
				noise := noiseGen.Eval2(seedX+float64(dx)*noiseFreq, seedY+float64(dy)*noiseFreq)
				noiseContribution := noise * (lakeEdgeRoughness / 100.0)
				return noiseContribution - distPenalty
			}

			return -distPenalty
		}

		heap.Push(pq, &lakePixel{point: startPt, score: getScore(startPt)})
		visited[startPt] = true

		// Grow lake to target size
		lakeCount := 0
		for pq.Len() > 0 && lakeCount < targetPixelsPerLake {
			current := heap.Pop(pq).(*lakePixel)

			canvas.Set(current.point.X, current.point.Y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			currentLake = append(currentLake, current.point)
			lakeCount++

			// Add neighboring pixels to growth queue
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

// River represents a river on the map
type River struct {
	Width      float64
	Start, End image.Point
	Points     []image.Point
}

// GenerateRivers creates rivers flowing across the map from edge to edge
func GenerateRivers(width, height, numRivers int, minWidth, maxWidth, curvyness float64, inputImage image.Image, lakes [][]image.Point, seed int64, heightmap image.Image, riverWidthVariability, riverEdgeRoughness float64) (image.Image, []image.Point) {
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

	// Build water pixel lookup maps
	isWater := make(map[image.Point]bool)
	lakePixelMap := make(map[image.Point]int)
	for i, lake := range lakes {
		for _, p := range lake {
			isWater[p] = true
			lakePixelMap[p] = i
		}
	}

	// Create rivers with progressively varying widths
	rivers := make([]River, numRivers)
	for i := range numRivers {
		widthPercent := float64(i) / float64(numRivers-1)
		if numRivers == 1 {
			widthPercent = 0.5
		}
		rivers[i].Width = maxWidth - widthPercent*(maxWidth-minWidth)
	}

	// Sort rivers by width in descending order
	sort.Slice(rivers, func(i, j int) bool {
		return rivers[i].Width > rivers[j].Width
	})

	numControlPoints := max(int(avgDim*0.03), 60)

	// Generate each river
	for i := range rivers {
		r := &rivers[i]

		// Pick random start and end edges
		startEdge := randSrc.Intn(4)
		endEdge := (startEdge + randSrc.Intn(3) + 1) % 4

		r.Start = getPointOnEdge(width, height, startEdge, randSrc)
		r.End = getPointOnEdge(width, height, endEdge, randSrc)

		// Calculate river path with curves
		path := calculateRiverPath(r.Start, r.End, curvyness/100.0, avgDim, randSrc, numControlPoints)

		// Check for intersections with existing water
		for _, p := range path {
			if isWater[p] {
				if lakeIndex, isLake := lakePixelMap[p]; isLake {
					// End river at lake center if it intersects
					lakeCenter := findCenter(lakes[lakeIndex])
					r.End = lakeCenter
				} else {
					// End river at intersection with another river
					r.End = p
				}
				path = calculateRiverPath(r.Start, r.End, curvyness/100.0, avgDim, randSrc, numControlPoints)
				break
			}
		}

		// Draw river on canvas
		riverWidthPx := (r.Width / 100.0) * avgDim
		radius := riverWidthPx / 2.0

		for _, p := range path {
			drawCircle(canvas, p, radius, color.RGBA{R: 0, G: 0, B: 255, A: 255}, &allRiverPixels, isWater, heightmap, riverWidthVariability, riverEdgeRoughness)
		}
		r.Points = path
	}

	return canvas, allRiverPixels
}

// bresenhamRiver draws a line between control points using Bresenham's algorithm
func bresenhamRiver(path []image.Point) []image.Point {
	if len(path) < 2 {
		return path
	}

	var fullPath []image.Point
	for i := 0; i < len(path)-1; i++ {
		p1, p2 := path[i], path[i+1]
		dx, dy := p2.X-p1.X, p2.Y-p1.Y
		absDx, absDy := int(math.Abs(float64(dx))), int(math.Abs(float64(dy)))
		sx, sy := 1, 1
		if dx < 0 {
			sx = -1
		}
		if dy < 0 {
			sy = -1
		}
		err := absDx - absDy

		x, y := p1.X, p1.Y
		for {
			fullPath = append(fullPath, image.Point{X: x, Y: y})
			if x == p2.X && y == p2.Y {
				break
			}
			e2 := 2 * err
			if e2 > -absDy {
				err -= absDy
				x += sx
			}
			if e2 < absDx {
				err += absDx
				y += sy
			}
		}
	}
	return fullPath
}

// calculateRiverPath computes a curved path for a river using sine waves
func calculateRiverPath(start, end image.Point, curvyness, avgDim float64, randSrc *rand.Rand, numControlPoints int) []image.Point {
	dx := end.X - start.X
	dy := end.Y - start.Y
	dist := math.Sqrt(float64(dx*dx + dy*dy))

	if dist == 0 {
		return []image.Point{start}
	}

	if curvyness == 0 {
		return bresenhamRiver([]image.Point{start, end})
	}

	// Use multiple sine waves at different frequencies for natural curves
	type wave struct {
		amplitude float64
		numWaves  float64
		phase     float64
	}

	waves := make([]wave, 3)
	amp := (avgDim / 10.0) * curvyness
	mainWavelength := avgDim / 4.0
	if mainWavelength < 1 {
		mainWavelength = 1
	}
	baseNumWaves := (dist / mainWavelength) * curvyness

	for i := 0; i < 3; i++ {
		freqMultiplier := 1.0 + float64(i)
		randomizedNumWaves := baseNumWaves * freqMultiplier * (0.75 + randSrc.Float64()*0.5)
		waves[i] = wave{
			amplitude: amp,
			numWaves:  randomizedNumWaves,
			phase:     randSrc.Float64() * 2 * math.Pi,
		}
		amp /= 3
	}

	// Generate control points along the path
	controlPoints := make([]image.Point, numControlPoints+1)
	for i := 0; i <= numControlPoints; i++ {
		t := float64(i) / float64(numControlPoints)
		x := float64(start.X) + t*float64(dx)
		y := float64(start.Y) + t*float64(dy)
		perpX, perpY := -float64(dy)/dist, float64(dx)/dist

		totalOffset := 0.0
		for _, w := range waves {
			totalOffset += math.Sin(t*w.numWaves*2*math.Pi+w.phase) * w.amplitude
		}
		totalOffset *= math.Sin(t * math.Pi)

		x += totalOffset * perpX
		y += totalOffset * perpY
		controlPoints[i] = image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
	}

	// Create final path using Bresenham between control points
	return bresenhamRiver(controlPoints)
}

// findCenter calculates the center point of a set of pixels
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

// getPointOnEdge returns a random point on the specified map edge
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

// drawCircle draws a circular river cross-section with sine wave edge roughening
func drawCircle(img *image.RGBA, center image.Point, radius float64, c color.Color, pixels *[]image.Point, isWater map[image.Point]bool, heightmap image.Image, riverWidthVariability, riverEdgeRoughness float64) {
	bounds := img.Bounds()

	largeAmplitude := (radius * 0.5) * (riverWidthVariability / 100.0)
	smallAmplitude := largeAmplitude * (riverEdgeRoughness / 100.0)

	for y := int(math.Floor(float64(center.Y) - radius)); y <= int(math.Ceil(float64(center.Y)+radius)); y++ {
		for x := int(math.Floor(float64(center.X) - radius)); x <= int(math.Ceil(float64(center.X)+radius)); x++ {
			p := image.Point{X: x, Y: y}
			if !p.In(bounds) {
				continue
			}

			dx, dy := float64(x-center.X), float64(y-center.Y)
			dist := math.Sqrt(dx*dx + dy*dy)

			// Apply dual sine waves for edge roughness
			positionPhase := float64(x)*0.008 + float64(y)*0.012
			largeWave := math.Sin(positionPhase) * largeAmplitude
			smallWave := math.Sin(positionPhase*3.5) * smallAmplitude
			waveOffset := largeWave + smallWave

			effectiveRadius := radius + waveOffset

			if dist <= effectiveRadius {
				if !isWater[p] {
					img.Set(x, y, c)
					*pixels = append(*pixels, p)
					isWater[p] = true
				}
			}
		}
	}
}
