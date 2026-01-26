package main

import (
	"container/heap"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"time"

	"github.com/aquilax/go-perlin"
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

// GenerateLakes creates a specific number of lakes, each covering a specific percentage of the total image area.
// It uses a priority-based growth algorithm to ensure each lake is a single continuous component with organic edges.
func GenerateLakes(width, height, numLakes int, lakeSize float64) (image.Image, []image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	// Global map to track which pixels are already water to prevent duplicate darkening
	isWater := make(map[image.Point]bool)
	var allLakePixels []image.Point

	if numLakes <= 0 || lakeSize <= 0 {
		return canvas, allLakePixels
	}

	totalArea := float64(width * height)
	targetPixelsPerLake := int(math.Round(totalArea * (lakeSize / 100.0)))
	if targetPixelsPerLake <= 0 {
		targetPixelsPerLake = 1
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// One octave for maximum smoothness (no fractal detail that creates islands)
	p := perlin.NewPerlin(2.0, 2.0, 1, r.Int63())

	for i := 0; i < numLakes; i++ {
		// Unique seed for this specific lake
		seedX := r.Float64() * 10000.0
		seedY := r.Float64() * 10000.0

		// Choose a random seed point
		startPt := image.Point{X: r.Intn(width), Y: r.Intn(height)}

		pq := &priorityQueue{}
		heap.Init(pq)

		// track pixels already considered for THIS lake
		visited := make(map[image.Point]bool)

		// Scale noise relative to expected lake size to maintain look
		radius := math.Sqrt(float64(targetPixelsPerLake) / math.Pi)
		// Much lower frequency to avoid islands and thin peninsulas
		noiseFreq := 0.01 + (0.2 / (radius + 1.0))

		// Helper to calculate score
		getScore := func(pt image.Point) float64 {
			dx, dy := pt.X-startPt.X, pt.Y-startPt.Y
			dist := math.Sqrt(float64(dx*dx + dy*dy))

			// Noise component
			noise := p.Noise2D(seedX+float64(dx)*noiseFreq, seedY+float64(dy)*noiseFreq)

			// Non-linear distance penalty: very low near center, increases rapidly at edge
			// This makes the center much more "solid"
			distPenalty := math.Pow(dist/radius, 2.0)

			return noise - distPenalty
		}

		// Push starting point
		heap.Push(pq, &lakePixel{point: startPt, score: getScore(startPt)})
		visited[startPt] = true

		lakeCount := 0
		for pq.Len() > 0 && lakeCount < targetPixelsPerLake {
			// Pop the highest scoring frontier pixel
			current := heap.Pop(pq).(*lakePixel)

			// Add to canvas and global list
			canvas.Set(current.point.X, current.point.Y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			if !isWater[current.point] {
				isWater[current.point] = true
				allLakePixels = append(allLakePixels, current.point)
			}
			lakeCount++

			// Add neighbors to frontier
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					neighbor := image.Point{X: current.point.X + dx, Y: current.point.Y + dy}

					// Bounds check
					if neighbor.X < 0 || neighbor.X >= width || neighbor.Y < 0 || neighbor.Y >= height {
						continue
					}

					if !visited[neighbor] {
						visited[neighbor] = true
						heap.Push(pq, &lakePixel{
							point: neighbor,
							score: getScore(neighbor),
						})
					}
				}
			}
		}
	}

	return canvas, allLakePixels
}

// DarkenLakeAreas applies a visual darkening effect to the heightmap where lakes exist.
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
