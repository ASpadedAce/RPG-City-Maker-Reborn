package main

import (
	"container/heap"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"

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

// GenerateLakes creates lakes on the map using a priority queue growth algorithm
func GenerateLakes(width, height, numLakes int, lakeSizeLower, lakeSizeUpper float64, seed int64, lakeEdgeRoughness float64, lakeShape string) (image.Image, [][]image.Point) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	if numLakes <= 0 || lakeSizeLower <= 0 {
		return canvas, nil
	}

	var allLakes [][]image.Point
	randSrc := rand.New(rand.NewSource(seed))

	totalArea := float64(width * height)
	noiseGen := opensimplex.New(seed)

	type lakeBlob struct {
		dx, dy float64
		a, b   float64
		angle  float64
	}

	type lakeData struct {
		blobs          []lakeBlob
		primaryRadius  float64
		boundingRadius float64
		targetPixels   int
		center         image.Point
		placed         bool
	}

	lakesToPlace := make([]*lakeData, numLakes)

	// Phase 1: Generate parameters for all lakes
	for i := range numLakes {
		l := &lakeData{}

		getRadius := func() float64 {
			s := lakeSizeLower
			if lakeSizeUpper > lakeSizeLower {
				s = lakeSizeLower + randSrc.Float64()*(lakeSizeUpper-lakeSizeLower)
			}
			// Maintain the same scale heuristic as original code
			pixels := totalArea * (s / 100.0) / 2
			if pixels < 1 {
				pixels = 1
			}
			return math.Sqrt(pixels / math.Pi)
		}

		switch lakeShape {
		case "oval":
			r1 := getRadius()
			r2 := getRadius()
			angle := randSrc.Float64() * math.Pi * 2
			l.blobs = append(l.blobs, lakeBlob{0, 0, r1, r2, angle})
			l.primaryRadius = (r1 + r2) / 2
		case "procedural":
			complexity := 2 + randSrc.Intn(3) // 2 to 4 blobs
			r1 := getRadius()
			r2 := r1
			if randSrc.Float64() > 0.5 {
				r2 = getRadius()
			}
			angle := randSrc.Float64() * math.Pi * 2
			l.blobs = append(l.blobs, lakeBlob{0, 0, r1, r2, angle})
			l.primaryRadius = (r1 + r2) / 2

			for k := 1; k < complexity; k++ {
				parent := l.blobs[randSrc.Intn(len(l.blobs))]
				subR1 := getRadius() * 0.7
				subR2 := subR1
				if randSrc.Float64() > 0.5 {
					subR2 = getRadius() * 0.7
				}
				subAngle := randSrc.Float64() * math.Pi * 2
				dir := randSrc.Float64() * math.Pi * 2
				dist := (parent.a + subR1) * 0.6 // Overlap
				newX := parent.dx + math.Cos(dir)*dist
				newY := parent.dy + math.Sin(dir)*dist
				l.blobs = append(l.blobs, lakeBlob{newX, newY, subR1, subR2, subAngle})
			}
		default: // "circle"
			r := getRadius()
			l.blobs = append(l.blobs, lakeBlob{0, 0, r, r, 0})
			l.primaryRadius = r
		}

		estimatedArea := 0.0
		maxBlobDist := 0.0
		for _, b := range l.blobs {
			estimatedArea += math.Pi * b.a * b.b
			dist := math.Sqrt(b.dx*b.dx+b.dy*b.dy) + math.Max(b.a, b.b)
			if dist > maxBlobDist {
				maxBlobDist = dist
			}
		}
		if len(l.blobs) > 1 {
			estimatedArea *= 0.8
		}
		l.targetPixels = int(estimatedArea)
		if l.targetPixels <= 0 {
			l.targetPixels = 1
		}
		l.boundingRadius = maxBlobDist * 1.2
		lakesToPlace[i] = l
	}

	// Phase 2: Initial Placement (Tight Packing)
	var placedLakes []*lakeData
	for i, l := range lakesToPlace {
		placed := false
		// Try random placement first
		for attempt := 0; attempt < 100; attempt++ {
			cx := randSrc.Intn(width)
			cy := randSrc.Intn(height)

			// Relaxed boundary check: center can be anywhere, but let's keep it somewhat reasonable
			// Allow center to be outside by radius/2
			margin := int(l.boundingRadius / 2)
			if cx < -margin || cx >= width+margin || cy < -margin || cy >= height+margin {
				continue
			}

			overlap := false
			for _, other := range placedLakes {
				dx := float64(cx - other.center.X)
				dy := float64(cy - other.center.Y)
				dist := math.Sqrt(dx*dx + dy*dy)
				if dist < (l.boundingRadius + other.boundingRadius) {
					overlap = true
					break
				}
			}

			if !overlap {
				l.center = image.Point{X: cx, Y: cy}
				l.placed = true
				placed = true
				break
			}
		}

		// Fallback: Orbit existing lakes (Tangent placement)
		if !placed && len(placedLakes) > 0 {
			indices := randSrc.Perm(len(placedLakes))
			for _, idx := range indices {
				targetLake := placedLakes[idx]
				targetDist := targetLake.boundingRadius + l.boundingRadius // Touching

				const angleSteps = 36
				startAngle := randSrc.Float64() * 2 * math.Pi

				for k := 0; k < angleSteps; k++ {
					angle := startAngle + (float64(k)/float64(angleSteps))*2*math.Pi
					cx := int(float64(targetLake.center.X) + math.Cos(angle)*targetDist)
					cy := int(float64(targetLake.center.Y) + math.Sin(angle)*targetDist)

					margin := int(l.boundingRadius / 2)
					if cx < -margin || cx >= width+margin || cy < -margin || cy >= height+margin {
						continue
					}

					overlap := false
					for _, other := range placedLakes {
						dx := float64(cx - other.center.X)
						dy := float64(cy - other.center.Y)
						dist := math.Sqrt(dx*dx + dy*dy)
						if dist < (l.boundingRadius + other.boundingRadius) { // Touching check
							overlap = true
							break
						}
					}

					if !overlap {
						l.center = image.Point{X: cx, Y: cy}
						l.placed = true
						placed = true
						break
					}
				}
				if placed {
					break
				}
			}
		}

		if placed {
			placedLakes = append(placedLakes, l)
		} else {
			// Discard lake if it really can't fit
			lakesToPlace[i] = nil
		}
	}

	// Phase 3: Scattering (Relaxation)
	avgDim := float64(width+height) / 2.0
	minGap := avgDim * 0.01
	iterations := len(placedLakes) * 100

	for k := 0; k < iterations; k++ {
		if len(placedLakes) == 0 {
			break
		}
		idx := randSrc.Intn(len(placedLakes))
		l := placedLakes[idx]

		// Propose new random position
		cx := randSrc.Intn(width)
		cy := randSrc.Intn(height)

		margin := int(l.boundingRadius / 2)
		if cx < -margin || cx >= width+margin || cy < -margin || cy >= height+margin {
			continue
		}

		valid := true
		for j, other := range placedLakes {
			if idx == j {
				continue
			}
			dx := float64(cx - other.center.X)
			dy := float64(cy - other.center.Y)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < (l.boundingRadius + other.boundingRadius + minGap) {
				valid = false
				break
			}
		}

		if valid {
			l.center = image.Point{X: cx, Y: cy}
		}
	}

	// Phase 4: Grow lakes at final positions
	globalVisited := make(map[image.Point]bool)
	seedX := randSrc.Float64() * 10000.0
	seedY := randSrc.Float64() * 10000.0

	for _, l := range placedLakes {
		if l == nil || !l.placed {
			continue
		}

		var currentLake []image.Point
		startPt := l.center

		// Check if start point is within strict bounds for drawing initiation
		if !startPt.In(image.Rect(0, 0, width, height)) {
			// Try to find a point within the lake radius that is on the map
			found := false
			for r := 0; r < int(l.boundingRadius); r++ {
				for angle := 0.0; angle < 2*math.Pi; angle += 0.5 {
					nx := startPt.X + int(float64(r)*math.Cos(angle))
					ny := startPt.Y + int(float64(r)*math.Sin(angle))
					pt := image.Point{nx, ny}
					if pt.In(image.Rect(0, 0, width, height)) {
						startPt = pt
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				continue // Lake is completely off-screen or unplaceable
			}
		}

		noiseFreq := 0.01 + (0.2 / (l.primaryRadius + 1.0))

		getScore := func(pt image.Point) float64 {
			dxGlobal := float64(pt.X - l.center.X)
			dyGlobal := float64(pt.Y - l.center.Y)

			minNormalizedDist := 1e9

			for _, b := range l.blobs {
				bdx := dxGlobal - b.dx
				bdy := dyGlobal - b.dy

				cosA := math.Cos(-b.angle)
				sinA := math.Sin(-b.angle)
				rx := bdx*cosA - bdy*sinA
				ry := bdx*sinA + bdy*cosA

				d := math.Sqrt(math.Pow(rx/b.a, 2) + math.Pow(ry/b.b, 2))
				if d < minNormalizedDist {
					minNormalizedDist = d
				}
			}

			distPenalty := math.Pow(minNormalizedDist, 3.0)

			if lakeEdgeRoughness > 0 {
				noise := noiseGen.Eval2(seedX+float64(dxGlobal)*noiseFreq, seedY+float64(dyGlobal)*noiseFreq)
				noiseContribution := noise * (lakeEdgeRoughness / 100.0)
				return noiseContribution - distPenalty
			}

			return -distPenalty
		}

		pq := &priorityQueue{}
		heap.Init(pq)
		heap.Push(pq, &lakePixel{point: startPt, score: getScore(startPt)})
		globalVisited[startPt] = true

		lakeCount := 0
		for pq.Len() > 0 && lakeCount < l.targetPixels {
			current := heap.Pop(pq).(*lakePixel)

			// Only draw if on canvas
			if current.point.In(image.Rect(0, 0, width, height)) {
				canvas.Set(current.point.X, current.point.Y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
				currentLake = append(currentLake, current.point)
			}
			lakeCount++

			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					neighbor := image.Point{X: current.point.X + dx, Y: current.point.Y + dy}

					if globalVisited[neighbor] {
						continue
					}
					// Allow growth slightly off-screen to ensure shape consistency, but don't track too far
					if neighbor.X < -int(l.boundingRadius) || neighbor.X >= width+int(l.boundingRadius) ||
						neighbor.Y < -int(l.boundingRadius) || neighbor.Y >= height+int(l.boundingRadius) {
						continue
					}

					globalVisited[neighbor] = true
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

// GenerateRivers creates rivers flowing across the map from edge to edge
func GenerateRivers(width, height, numRivers int, minWidth, maxWidth, curvyness float64, inputImage image.Image, lakes [][]image.Point, seed int64, heightmap image.Image, riverWidthVariability, riverEdgeRoughness float64) (image.Image, *PixelMask) {
	if numRivers == 0 {
		return inputImage, nil
	}

	canvas, ok := inputImage.(*image.RGBA)
	if !ok {
		canvas = image.NewRGBA(inputImage.Bounds())
		draw.Draw(canvas, canvas.Bounds(), inputImage, image.Point{}, draw.Src)
	}

	riverMask := NewPixelMask(width, height)
	randSrc := rand.New(rand.NewSource(seed))
	avgDim := float64(width+height) / 2.0

	// Build water pixel lookup maps
	isWater := BuildMaskFromLakes(width, height, lakes)
	lakePixelMap := make(map[image.Point]int)
	for i, lake := range lakes {
		for _, p := range lake {
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
			if isWater.GetPoint(p) {
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
			drawCircle(canvas, p, radius, color.RGBA{R: 0, G: 0, B: 255, A: 255}, riverMask, isWater, heightmap, riverWidthVariability, riverEdgeRoughness)
		}
		r.Points = path
	}

	return canvas, riverMask
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
func drawCircle(img *image.RGBA, center image.Point, radius float64, c color.Color, riverMask, isWater *PixelMask, heightmap image.Image, riverWidthVariability, riverEdgeRoughness float64) {
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
				if !isWater.GetPoint(p) {
					img.Set(x, y, c)
					riverMask.SetPoint(p)
					isWater.SetPoint(p)
				}
			}
		}
	}
}
