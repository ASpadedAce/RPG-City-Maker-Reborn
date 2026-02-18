package main

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"runtime"
	"sync"

	"github.com/ojrac/opensimplex-go"
)

type riverParams struct {
	EdgeBandRatio     float64
	RoughnessStrength float64
	IslandAttemptProb float64
	IslandSeedChance  float64
	MinIslandSize     int
	MaxIslandSize     int
	WaterLevelBias    float64
	NoiseFrequency    float64
	KeepInnerFraction float64
	MaxWorkers        int
	MinWidthPx        float64
	MaxWidthPx        float64
}

// computeSinWaveEdgeOffset computes dual sine wave edge roughening for realistic river banks
func computeSinWaveEdgeOffset(absX, absY int, largeAmplitude, smallAmplitude float64) float64 {
	positionPhase := float64(absX)*0.008 + float64(absY)*0.012
	largeWave := math.Sin(positionPhase) * largeAmplitude
	smallWave := math.Sin(positionPhase*3.5) * smallAmplitude
	return largeWave + smallWave
}

// RasterizeAndRoughenRiver rasterizes a river path with natural edge roughening and optional islands
func RasterizeAndRoughenRiver(canvas *image.RGBA, path []image.Point, riverWidthPx float64, heightmap image.Image, isWater map[image.Point]bool, seed int64, minWidthPx, maxWidthPx float64) []image.Point {
	if canvas == nil || len(path) == 0 || riverWidthPx <= 0 {
		return nil
	}

	params := riverParams{
		EdgeBandRatio:     0.6,
		RoughnessStrength: 0.65,
		IslandAttemptProb: 0.07,
		IslandSeedChance:  0.0025,
		MinIslandSize:     8,
		MaxIslandSize:     800,
		WaterLevelBias:    0.02,
		NoiseFrequency:    0.02,
		KeepInnerFraction: 0.85,
		MaxWorkers:        0,
		MinWidthPx:        minWidthPx,
		MaxWidthPx:        maxWidthPx,
	}

	bounds := canvas.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()

	heightGrid := precomputeHeightGrid(heightmap, imgW, imgH)

	radius := riverWidthPx / 2.0
	edgeBand := radius * params.EdgeBandRatio
	expand := int(math.Ceil(radius + edgeBand + 2))

	minX, minY := imgW, imgH
	maxX, maxY := 0, 0
	for _, p := range path {
		if p.X < minX {
			minX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	minX -= expand
	minY -= expand
	maxX += expand
	maxY += expand
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= imgW {
		maxX = imgW - 1
	}
	if maxY >= imgH {
		maxY = imgH - 1
	}

	bw := maxX - minX + 1
	bh := maxY - minY + 1
	if bw <= 0 || bh <= 0 {
		return nil
	}

	baseMask := make([]uint8, bw*bh)

	radiusSq := radius * radius
	for _, c := range path {
		cx := c.X - minX
		cy := c.Y - minY
		if cx < -int(radius) || cx > bw+int(radius) || cy < -int(radius) || cy > bh+int(radius) {
			continue
		}
		minRx := int(math.Max(0, float64(cx)-radius))
		maxRx := int(math.Min(float64(bw-1), float64(cx)+radius))
		minRy := int(math.Max(0, float64(cy)-radius))
		maxRy := int(math.Min(float64(bh-1), float64(cy)+radius))
		for yy := minRy; yy <= maxRy; yy++ {
			for xx := minRx; xx <= maxRx; xx++ {
				dx := float64(xx - cx)
				dy := float64(yy - cy)
				if dx*dx+dy*dy <= radiusSq {
					baseMask[yy*bw+xx] = 1
				}
			}
		}
	}

	dist := chamferDistanceField(baseMask, bw, bh)

	noise := opensimplex.New(seed)
	noiseFreq := params.NoiseFrequency

	innerKeepRadius := radius * params.KeepInnerFraction

	finalMask := make([]uint8, bw*bh)

	workers := params.MaxWorkers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	var wg sync.WaitGroup
	rowsPerWorker := (bh + workers - 1) / workers
	randBase := rand.New(rand.NewSource(seed))

	heightWeight := 2.0 * params.RoughnessStrength
	distWeight := params.RoughnessStrength
	noiseWeight := 0.5 * params.RoughnessStrength

	largeAmplitude := params.MaxWidthPx - params.MinWidthPx
	smallAmplitude := largeAmplitude / 4.0

	for wi := 0; wi < workers; wi++ {
		startY := wi * rowsPerWorker
		endY := startY + rowsPerWorker
		if endY > bh {
			endY = bh
		}
		if startY >= endY {
			continue
		}
		wg.Add(1)
		go func(startY, endY, workerID int) {
			defer wg.Done()
			localRand := rand.New(rand.NewSource(randBase.Int63() + int64(workerID)*7919))
			for y := startY; y < endY; y++ {
				for x := 0; x < bw; x++ {
					idx := y*bw + x
					if baseMask[idx] == 1 {
						d := dist[idx]
						absX := x + minX
						absY := y + minY
						if d <= float32(innerKeepRadius) {
							finalMask[idx] = 1
							continue
						}

						sinWaveOffset := computeSinWaveEdgeOffset(absX, absY, largeAmplitude, smallAmplitude)
						effectiveInnerRadius := innerKeepRadius + sinWaveOffset

						normDist := float64((float32(d) - float32(effectiveInnerRadius)) / float32(edgeBand))
						if normDist < 0 {
							normDist = 0
						}
						if normDist > 1 {
							normDist = 1
						}

						heightVal := sampleHeightGrid(heightGrid, imgW, imgH, absX, absY)
						heightAdj := float64(heightVal) - params.WaterLevelBias

						noiseVal := noise.Eval2(float64(absX)*noiseFreq, float64(absY)*noiseFreq)
						noiseNorm := (noiseVal + 1.0) / 2.0

						score := distWeight*normDist + heightWeight*heightAdj + noiseWeight*(noiseNorm-0.5)

						threshold := 0.35 + 0.5*params.RoughnessStrength
						if localRand.Float64() < 0.0005 {
							score += (localRand.Float64() - 0.5) * 0.2
						}

						if score < threshold {
							finalMask[idx] = 1
						} else {
							finalMask[idx] = 0
						}
					}
				}
			}
		}(startY, endY, wi)
	}
	wg.Wait()

	randForIsland := rand.New(rand.NewSource(seed + 1234567))
	tryIslands := randForIsland.Float64() < params.IslandAttemptProb
	if tryIslands {
		generateIslandsInMask(finalMask, bw, bh, minX, minY, heightGrid, imgW, imgH, &params, seed+4242)
	}

	var added []image.Point
	for y := 0; y < bh; y++ {
		absY := y + minY
		for x := 0; x < bw; x++ {
			absX := x + minX
			pt := image.Point{X: absX, Y: absY}
			if !pt.In(bounds) {
				continue
			}
			idx := y*bw + x
			if finalMask[idx] == 1 && !isWater[pt] {
				canvas.Set(absX, absY, color.RGBA{R: 0, G: 0, B: 255, A: 255})
				isWater[pt] = true
				added = append(added, pt)
			}
		}
	}

	removeSpeckles(&finalMask, bw, bh, 2)

	return added
}

// precomputeHeightGrid converts heightmap to normalized float32 grid
func precomputeHeightGrid(hmap image.Image, width, height int) []float32 {
	out := make([]float32, width*height)
	if hmap == nil {
		for i := range out {
			out[i] = 0.5
		}
		return out
	}
	b := hmap.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			absX := x + b.Min.X
			absY := y + b.Min.Y
			r, _, _, _ := hmap.At(absX, absY).RGBA()
			val := float32(r) / 65535.0
			out[y*width+x] = val
		}
	}
	return out
}

// sampleHeightGrid safely samples height at coordinates
func sampleHeightGrid(grid []float32, width, height, x, y int) float32 {
	if x < 0 || x >= width || y < 0 || y >= height {
		return 0.5
	}
	return grid[y*width+x]
}

// chamferDistanceField computes fast approximate distance from any pixel to centerline
func chamferDistanceField(baseMask []uint8, w, h int) []float32 {
	const maxF = 1e6
	dist := make([]float32, w*h)

	for i := 0; i < w*h; i++ {
		if baseMask[i] == 1 {
			dist[i] = 0
		} else {
			dist[i] = maxF
		}
	}

	// Forward pass
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if dist[i] == 0 {
				continue
			}
			if x > 0 {
				v := dist[i-1] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			if y > 0 {
				v := dist[i-w] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			if x > 0 && y > 0 {
				v := dist[i-w-1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
			if x < w-1 && y > 0 {
				v := dist[i-w+1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
		}
	}

	// Backward pass
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			i := y*w + x
			if x < w-1 {
				v := dist[i+1] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			if y < h-1 {
				v := dist[i+w] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			if x < w-1 && y < h-1 {
				v := dist[i+w+1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
			if x > 0 && y < h-1 {
				v := dist[i+w-1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
		}
	}

	return dist
}

// generateIslandsInMask creates small islands inside water areas
func generateIslandsInMask(mask []uint8, bw, bh, minX, minY int, heightGrid []float32, fullW, fullH int, params *riverParams, seed int64) {
	r := rand.New(rand.NewSource(seed))
	type pt struct{ x, y int }
	candidates := make([]pt, 0)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			idx := y*bw + x
			if mask[idx] != 1 {
				continue
			}
			absX := x + minX
			absY := y + minY
			hv := sampleHeightGrid(heightGrid, fullW, fullH, absX, absY)
			if float64(hv) > params.WaterLevelBias+0.03 {
				if r.Float64() < params.IslandSeedChance {
					candidates = append(candidates, pt{x, y})
				}
			}
		}
	}
	if len(candidates) == 0 {
		return
	}

	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })

	visited := make([]uint8, bw*bh)

	for _, c := range candidates {
		ci := c.y*bw + c.x
		if visited[ci] != 0 {
			continue
		}
		maxSize := params.MaxIslandSize
		minSize := params.MinIslandSize
		targetSize := minSize + r.Intn(maxSize-minSize+1)

		queue := []pt{{c.x, c.y}}
		visited[ci] = 1
		island := make([]pt, 0, targetSize)
		touchesEdge := false

		for qi := 0; qi < len(queue) && len(island) < targetSize; qi++ {
			p := queue[qi]
			absX := p.x + minX
			absY := p.y + minY
			hv := sampleHeightGrid(heightGrid, fullW, fullH, absX, absY)
			if float64(hv) < params.WaterLevelBias+0.01 {
				continue
			}
			island = append(island, p)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := p.x+dx, p.y+dy
					if nx < 0 || nx >= bw || ny < 0 || ny >= bh {
						touchesEdge = true
						continue
					}
					nidx := ny*bw + nx
					if visited[nidx] != 0 {
						continue
					}
					if mask[nidx] != 1 {
						continue
					}
					visited[nidx] = 1
					queue = append(queue, pt{nx, ny})
				}
			}
		}

		if touchesEdge {
			continue
		}

		if len(island) < minSize {
			continue
		}

		for _, p := range island {
			mask[p.y*bw+p.x] = 0
		}

		if r.Float64() < 0.7 {
			if r.Intn(3) == 0 {
				break
			}
		}
	}
}

// removeSpeckles removes tiny isolated water pixels
func removeSpeckles(mask *[]uint8, bw, bh, minNeighbors int) {
	arr := *mask
	out := make([]uint8, len(arr))
	copy(out, arr)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			idx := y*bw + x
			if arr[idx] == 0 {
				continue
			}
			count := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx := x + dx
					ny := y + dy
					if nx < 0 || nx >= bw || ny < 0 || ny >= bh {
						continue
					}
					if arr[ny*bw+nx] == 1 {
						count++
					}
				}
			}
			if count < minNeighbors {
				out[idx] = 0
			}
		}
	}
	copy(arr, out)
	*mask = arr
}

// maskToPoints converts mask to point slice for visualization
func maskToPoints(mask []uint8, bw, bh, minX, minY int) []image.Point {
	var pts []image.Point
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			if mask[y*bw+x] == 1 {
				pts = append(pts, image.Point{X: x + minX, Y: y + minY})
			}
		}
	}
	return pts
}

// clamp01 clamps value to 0..1 range
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
