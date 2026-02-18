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

// rivers.go
//
// New river roughening implementation that uses the heightmap to clip river edges,
// occasionally creates islands, and is designed to be efficient and multithreadable.
//
// This file exposes one main function intended to be called from the river generation
// pipeline in place of per-pixel painting: `RasterizeAndRoughenRiver`. It:
//  - rasterizes the river centerline into a local mask (bounding box)
//  - computes a fast distance field (chamfer approximation) from the centerline
//  - evaluates a heightmap-aware stochastic rule to remove/add edge pixels to roughen
//  - occasionally grows islands inside the river
//  - writes final water pixels back to the provided canvas and updates the provided isWater map
//
// Usage (conceptual):
//   addedPixels := RasterizeAndRoughenRiver(canvas, path, riverWidthPx, heightmap, isWater, seed)
//
// NOTE: Because the project already contained a `drawCircle` helper, this new pipeline
// is implemented as standalone routines in this file. To use it, replace the existing
// per-circle painting logic in `GenerateRivers` with a call to `RasterizeAndRoughenRiver`.
//
// The parameters below were chosen conservatively; tweak them to taste.

type riverParams struct {
	EdgeBandRatio     float64 // fraction of river radius used for roughening band (e.g. 0.6)
	RoughnessStrength float64 // 0..1 how aggressive clipping is at the edge
	IslandAttemptProb float64 // chance per-river to attempt islands
	IslandSeedChance  float64 // chance per-water-pixel to become an island seed candidate
	MinIslandSize     int     // minimum island pixel count
	MaxIslandSize     int     // maximum island pixel count
	WaterLevelBias    float64 // baseline water level in normalized height units [0..1]; small bias subtracted to favor water
	NoiseFrequency    float64 // frequency for simplex noise
	KeepInnerFraction float64 // fraction of inner radius always kept as channel (0..1)
	MaxWorkers        int     // concurrency limit (0 means runtime.NumCPU())
	MinWidthPx        float64 // minimum river width in pixels (for sin wave amplitude calculation)
	MaxWidthPx        float64 // maximum river width in pixels (for sin wave amplitude calculation)
}

// computeSinWaveEdgeOffset computes the radial offset for river edge roughening
// using dual sine waves. The larger wave has amplitude based on the difference
// between max and min river widths, and the smaller wave is a quarter of that amplitude.
// This creates realistic undulating river banks with both large and small-scale variations.
func computeSinWaveEdgeOffset(absX, absY int, largeAmplitude, smallAmplitude float64) float64 {
	// Use position to create phase for the sine waves
	// Position phase creates variation as we move through the image
	positionPhase := float64(absX)*0.008 + float64(absY)*0.012

	// Large wave: slower frequency for major width variations along the bank
	largeWave := math.Sin(positionPhase) * largeAmplitude

	// Small wave: faster frequency for subtle and natural bank details
	smallWave := math.Sin(positionPhase*3.5) * smallAmplitude

	// Return combined offset
	return largeWave + smallWave
}

// RasterizeAndRoughenRiver rasterizes a river path, roughens edges using the heightmap and dual sin waves,
// optionally creates islands, paints the final water into `canvas`, and marks pixels in `isWater`.
// It returns a slice of image.Point containing all newly added water pixels for this river.
//
// Parameters:
// - canvas: destination image (will be modified)
// - path: ordered centerline points for the river
// - riverWidthPx: nominal width in pixels
// - heightmap: heightmap image used to guide roughening (expects 0..1 grayscale via RGBA() conversion)
// - isWater: map used to record already-water pixels (prevents painting over lakes/rivers). This map will be updated.
// - seed: random seed to make generation deterministic
// - minWidthPx: minimum river width in pixels (used for sin wave amplitude calculation)
// - maxWidthPx: maximum river width in pixels (used for sin wave amplitude calculation)
func RasterizeAndRoughenRiver(canvas *image.RGBA, path []image.Point, riverWidthPx float64, heightmap image.Image, isWater map[image.Point]bool, seed int64, minWidthPx, maxWidthPx float64) []image.Point {
	if canvas == nil || len(path) == 0 || riverWidthPx <= 0 {
		return nil
	}

	// Default parameters - tweak as needed
	params := riverParams{
		EdgeBandRatio:     0.6,
		RoughnessStrength: 0.65,
		IslandAttemptProb: 0.07,
		IslandSeedChance:  0.0025,
		MinIslandSize:     8,
		MaxIslandSize:     800,
		WaterLevelBias:    0.02,
		NoiseFrequency:    0.02,
		KeepInnerFraction: 0.85, // keep central 85% of radius
		MaxWorkers:        0,
		MinWidthPx:        minWidthPx,
		MaxWidthPx:        maxWidthPx,
	}

	bounds := canvas.Bounds()
	imgW, imgH := bounds.Dx(), bounds.Dy()

	// Precompute normalized height grid for faster sampling.
	heightGrid := precomputeHeightGrid(heightmap, imgW, imgH)

	// Compute bounding box for path expanded by radius + edge band
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

	// Create base raster mask inside bounding box.
	// baseMask[i] == 1 means inside nominal river radius (before roughening).
	baseMask := make([]uint8, bw*bh)

	// Rasterize simple circular stamping for each center point into baseMask
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

	// Compute distance field (approximate Euclidean) from centerline (distance 0 at pixels inside baseMask)
	dist := chamferDistanceField(baseMask, bw, bh)

	// Prepare noise generator
	noise := opensimplex.New(seed)
	noiseFreq := params.NoiseFrequency

	// Determine inner keep radius (always keep central channel)
	innerKeepRadius := radius * params.KeepInnerFraction

	// Prepare final mask
	finalMask := make([]uint8, bw*bh)

	// Concurrency setup
	workers := params.MaxWorkers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	var wg sync.WaitGroup
	rowsPerWorker := (bh + workers - 1) / workers
	randBase := rand.New(rand.NewSource(seed))

	// Precompute some weights for the decision formula
	heightWeight := 2.0 * params.RoughnessStrength
	distWeight := params.RoughnessStrength
	noiseWeight := 0.5 * params.RoughnessStrength

	// Compute sin wave amplitudes for realistic edge roughening
	// Large amplitude is the difference between max and min river widths
	// Small amplitude is a quarter of the large amplitude for subtle bank details
	largeAmplitude := params.MaxWidthPx - params.MinWidthPx
	smallAmplitude := largeAmplitude / 4.0

	// Evaluate per-pixel decision in parallel
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
					// If already inside base mask, candidate for water
					if baseMask[idx] == 1 {
						// If within inner keep radius: keep always
						d := dist[idx]
						// dist is approximate pixels; we compare to innerKeepRadius
						absX := x + minX
						absY := y + minY
						if d <= float32(innerKeepRadius) {
							finalMask[idx] = 1
							continue
						}

						// Apply sin wave offset for realistic edge roughening
						sinWaveOffset := computeSinWaveEdgeOffset(absX, absY, largeAmplitude, smallAmplitude)
						effectiveInnerRadius := innerKeepRadius + sinWaveOffset

						// Compute influences
						// normalizedDist: 0 at effectiveInnerRadius, 1 at effectiveInnerRadius + edgeBand
						normDist := float64((float32(d) - float32(effectiveInnerRadius)) / float32(edgeBand))
						if normDist < 0 {
							normDist = 0
						}
						if normDist > 1 {
							normDist = 1
						}

						heightVal := sampleHeightGrid(heightGrid, imgW, imgH, absX, absY) // 0..1
						// Apply bias so slightly lower areas favor water
						heightAdj := float64(heightVal) - params.WaterLevelBias

						noiseVal := noise.Eval2(float64(absX)*noiseFreq, float64(absY)*noiseFreq) // -1 .. 1
						noiseNorm := (noiseVal + 1.0) / 2.0                                       // 0..1

						score := distWeight*normDist + heightWeight*heightAdj + noiseWeight*(noiseNorm-0.5)

						// Decision threshold: higher score means more likely land.
						threshold := 0.35 + 0.5*params.RoughnessStrength
						// Small stochastic factor to add natural variance
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

	// Optionally attempt islands with small probability
	randForIsland := rand.New(rand.NewSource(seed + 1234567))
	tryIslands := randForIsland.Float64() < params.IslandAttemptProb
	if tryIslands {
		generateIslandsInMask(finalMask, bw, bh, minX, minY, heightGrid, imgW, imgH, &params, seed+4242)
	}

	// Paint finalMask to canvas and collect pixels (only those not already water)
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

	// Small cleanup: remove tiny isolated water pixels (optional - lightweight)
	removeSpeckles(&finalMask, bw, bh, 2)

	return added
}

// precomputeHeightGrid converts the heightmap to a float32 grid [0..1] sized width*height.
func precomputeHeightGrid(hmap image.Image, width, height int) []float32 {
	out := make([]float32, width*height)
	if hmap == nil {
		// default flat
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

// sampleHeightGrid safe accessor
func sampleHeightGrid(grid []float32, width, height, x, y int) float32 {
	if x < 0 || x >= width || y < 0 || y >= height {
		return 0.5
	}
	return grid[y*width+x]
}

// chamferDistanceField computes a fast approximate distance (in pixels) from any pixel to the nearest
// baseMask==1 pixel. Distance is zero for pixels inside baseMask.
// This is a two-pass chamfer approximation (float), cheap and parallel friendly.
func chamferDistanceField(baseMask []uint8, w, h int) []float32 {
	const maxF = 1e6
	dist := make([]float32, w*h)

	// Initialize
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
			// check left
			if x > 0 {
				v := dist[i-1] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check top
			if y > 0 {
				v := dist[i-w] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check top-left
			if x > 0 && y > 0 {
				v := dist[i-w-1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check top-right
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
			// check right
			if x < w-1 {
				v := dist[i+1] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check bottom
			if y < h-1 {
				v := dist[i+w] + 1.0
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check bottom-right
			if x < w-1 && y < h-1 {
				v := dist[i+w+1] + 1.41421356
				if v < dist[i] {
					dist[i] = v
				}
			}
			// check bottom-left
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

// generateIslandsInMask will attempt to create small islands inside contiguous water areas.
// It modifies the mask in place (1=water, 0=land). The algorithm:
//   - choose candidate water pixels with slightly higher-than-water height
//   - use a small BFS flood constrained by height to form island patches
//   - reject patches that touch the bounding box edge (we want enclosed islands)
//   - enforce size limits
func generateIslandsInMask(mask []uint8, bw, bh, minX, minY int, heightGrid []float32, fullW, fullH int, params *riverParams, seed int64) {
	r := rand.New(rand.NewSource(seed))
	// Collect candidates
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
			// candidate if slightly higher than local water bias
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

	// Shuffle candidates to randomize island placement
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })

	visited := make([]uint8, bw*bh)

	for _, c := range candidates {
		ci := c.y*bw + c.x
		if visited[ci] != 0 {
			continue
		}
		// BFS grow island
		maxSize := params.MaxIslandSize
		minSize := params.MinIslandSize
		// randomize size a bit
		targetSize := minSize + r.Intn(maxSize-minSize+1)

		queue := []pt{{c.x, c.y}}
		visited[ci] = 1
		island := make([]pt, 0, targetSize)
		touchesEdge := false

		for qi := 0; qi < len(queue) && len(island) < targetSize; qi++ {
			p := queue[qi]
			absX := p.x + minX
			absY := p.y + minY
			// Height constraint: island must be above a modest threshold
			hv := sampleHeightGrid(heightGrid, fullW, fullH, absX, absY)
			if float64(hv) < params.WaterLevelBias+0.01 {
				continue
			}
			island = append(island, p)
			// Expand
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
					// Only grow into water pixels
					if mask[nidx] != 1 {
						continue
					}
					visited[nidx] = 1
					queue = append(queue, pt{nx, ny})
				}
			}
		}

		// If island touches bbox edge, reject it (we want enclosed islands)
		if touchesEdge {
			continue
		}

		// size check
		if len(island) < minSize {
			continue
		}

		// Carve the island: set mask pixels to 0 (land)
		for _, p := range island {
			mask[p.y*bw+p.x] = 0
		}

		// Optionally stop after creating a few islands to keep them rare
		if r.Float64() < 0.7 {
			// keep creating more sometimes, break otherwise
			if r.Intn(3) == 0 {
				break
			}
		}
	}
}

// removeSpeckles removes tiny isolated water components (erodes islands smaller than threshold).
// This is a simple pass that clears pixels that have fewer than minNeighbors water neighbors.
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

// (Optional) utility used for debug or visualization - not used directly in pipeline.
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

// small clamp helpers
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
