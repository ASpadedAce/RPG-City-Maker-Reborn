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

// GenerateLakes creates a specific number of lakes by dividing the image into chunks and placing one lake per chunk.
func GenerateLakes(width, height, numLakes int, lakeSizeLower, lakeSizeUpper float64, heightmap image.Image) (image.Image, []image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	if numLakes <= 0 || lakeSizeLower <= 0 {
		return canvas, nil
	}

	var allLakePixels []image.Point
	randSrc := rand.New(rand.NewSource(time.Now().UnixNano()))

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
	p := perlin.NewPerlin(2.0, 2.0, 1, randSrc.Int63())

	// 3. Generate a lake in a subset of the chunks
	for i := 0; i < numLakes; i++ {
		if i >= len(chunkIndices) {
			break
		}

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
			noise := p.Noise2D(seedX+float64(dx)*noiseFreq, seedY+float64(dy)*noiseFreq)
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
			allLakePixels = append(allLakePixels, current.point)
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
	}

	return canvas, allLakePixels
}

// DarkenLakeAreas applies a visual darkening effect to the heightmap where lakes exist.
func DarkenLakeAreas(heightmap image.Image, lakePixels []image.Point) image.Image {
	bounds := heightmap.Bounds()
	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)

	// Create a map for quick lookup of lake pixels
	isLake := make(map[image.Point]bool)
	for _, p := range lakePixels {
		isLake[p] = true
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !isLake[image.Point{X: x, Y: y}] {
				c := composite.At(x, y)
				r, g, b, a := c.RGBA()
				// Darken by 15%
				r = uint32(float64(r) * 0.85)
				g = uint32(float64(g) * 0.85)
				b = uint32(float64(b) * 0.85)
				composite.Set(x, y, color.RGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: uint16(a)})
			}
		}
	}

	return composite
}
