package main

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"sync"
)

const (
	minBuildingSizePercent  = 0.5
	maxBuildingSizePercent  = 25.0
	buildingSizePercentStep = 0.5
)

func averageImageDimension(width, height int) float64 {
	return (float64(width) + float64(height)) / 2.0
}

func clampBuildingSizePercent(v float64) float64 {
	if v < minBuildingSizePercent {
		return minBuildingSizePercent
	}
	if v > maxBuildingSizePercent {
		return maxBuildingSizePercent
	}
	return v
}

func snapBuildingSizePercent(v float64) float64 {
	v = clampBuildingSizePercent(v)
	steps := math.Round((v - minBuildingSizePercent) / buildingSizePercentStep)
	return clampBuildingSizePercent(minBuildingSizePercent + steps*buildingSizePercentStep)
}

func normalizeBuildingSizePercentRange(minPercent, maxPercent float64) (float64, float64) {
	minPercent = snapBuildingSizePercent(minPercent)
	maxPercent = snapBuildingSizePercent(maxPercent)
	if minPercent > maxPercent {
		minPercent, maxPercent = maxPercent, minPercent
	}
	return minPercent, maxPercent
}

func getBuildingSizeRangePixels(settings *Settings, width, height int) (float64, float64) {
	minPercent, maxPercent := normalizeBuildingSizePercentRange(settings.MinBuildingSize, settings.MaxBuildingSize)
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

func getMaxBuildingsForImage(settings *Settings, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	minSizePx, maxSizePx := getBuildingSizeRangePixels(settings, width, height)
	avgSizePx := (minSizePx + maxSizePx) / 2.0
	if avgSizePx < 1 {
		avgSizePx = 1
	}
	// Treat average building size as a side length to estimate per-building footprint.
	avgFootprint := avgSizePx * avgSizePx
	maxBuildings := int(float64(width*height) / avgFootprint)
	if maxBuildings < 1 {
		maxBuildings = 1
	}
	return maxBuildings
}

func capRequestedBuildingsToFit(settings *Settings, width, height int) int {
	maxBuildings := getMaxBuildingsForImage(settings, width, height)
	if settings.NumBuildings > maxBuildings {
		settings.NumBuildings = maxBuildings
	}
	return settings.NumBuildings
}

func sampleRandomLandPoint(width, height int, waterMask, roadMask *PixelMask, randSrc *rand.Rand) (image.Point, bool) {
	const randomTries = 128
	for i := 0; i < randomTries; i++ {
		p := image.Point{X: randSrc.Intn(width), Y: randSrc.Intn(height)}
		if !waterMask.GetPoint(p) && !roadMask.GetPoint(p) {
			return p, true
		}
	}
	if width <= 0 || height <= 0 {
		return image.Point{}, false
	}
	start := randSrc.Intn(width * height)
	total := width * height
	for i := 0; i < total; i++ {
		idx := (start + i) % total
		x := idx % width
		y := idx / width
		p := image.Point{X: x, Y: y}
		if !waterMask.GetPoint(p) && !roadMask.GetPoint(p) {
			return p, true
		}
	}
	return image.Point{}, false
}

// GenerateBuildings creates and places buildings on the map.
func GenerateBuildings(
	img *image.RGBA,
	width,
	height int,
	settings *Settings,
	roadAnchors []image.Point,
	waterMask,
	roadMask,
	exitRoadMask *PixelMask,
	seed int64,
) ([][]image.Point, *PixelMask) {
	// Early exit if no buildings are to be generated
	if settings.NumBuildings == 0 {
		return nil, nil
	}

	randSrc := rand.New(rand.NewSource(seed))
	buildingColor := color.RGBA{R: 128, G: 128, B: 128, A: 255} // Gray color for buildings
	if waterMask == nil {
		waterMask = NewPixelMask(width, height)
	}
	if roadMask == nil {
		roadMask = NewPixelMask(width, height)
	}
	if exitRoadMask == nil {
		exitRoadMask = NewPixelMask(width, height)
	}
	buildingMask := NewPixelMask(width, height)
	var buildings [][]image.Point
	var anchorPoints []image.Point
	var normalRoadAnchors []image.Point
	var exitRoadAnchors []image.Point

	if len(roadAnchors) > 0 {
		anchorPoints = roadAnchors
		for _, p := range anchorPoints {
			if exitRoadMask.GetPoint(p) {
				exitRoadAnchors = append(exitRoadAnchors, p)
			} else {
				normalRoadAnchors = append(normalRoadAnchors, p)
			}
		}
	}

	// Early exit if no anchors and no valid land.
	if len(anchorPoints) == 0 {
		if _, ok := sampleRandomLandPoint(width, height, waterMask, roadMask, randSrc); !ok {
			return nil, nil
		}
	} else {
		// Sort anchor points for deterministic placement
		sort.Slice(anchorPoints, func(i, j int) bool {
			if anchorPoints[i].Y != anchorPoints[j].Y {
				return anchorPoints[i].Y < anchorPoints[j].Y
			}
			return anchorPoints[i].X < anchorPoints[j].X
		})
	}

	// Main loop for placing buildings
	buildingsPlaced := 0
	searchTries := 100                                // Number of attempts to find a spot for a building around an anchor
	maxPlacementAttempts := settings.NumBuildings * 5 // To prevent infinite loops
	minBuildingSizePx, maxBuildingSizePx := getBuildingSizeRangePixels(settings, width, height)

	for buildingsPlaced < settings.NumBuildings && maxPlacementAttempts > 0 {
		maxPlacementAttempts--

		// Select an anchor point for the new building
		var anchor image.Point
		if randSrc.Float64() > settings.BuildingDistribution/100.0 {
			// Buildings should only rarely use exit-road anchors.
			useExitAnchor := len(exitRoadAnchors) > 0 && randSrc.Float64() < 0.02
			if useExitAnchor {
				anchor = exitRoadAnchors[randSrc.Intn(len(exitRoadAnchors))]
			} else if len(normalRoadAnchors) > 0 {
				anchor = normalRoadAnchors[randSrc.Intn(len(normalRoadAnchors))]
			} else if len(anchorPoints) > 0 {
				anchor = anchorPoints[randSrc.Intn(len(anchorPoints))]
			} else {
				p, ok := sampleRandomLandPoint(width, height, waterMask, roadMask, randSrc)
				if !ok {
					continue
				}
				anchor = p
			}
		} else {
			p, ok := sampleRandomLandPoint(width, height, waterMask, roadMask, randSrc)
			if !ok {
				continue // No land to place buildings on
			}
			anchor = p
		}

		// Search for a valid building location around the anchor
		for i := 0; i < searchTries; i++ {
			searchRadius := float64(i) * 2.0 // Search in expanding circles
			angle := randSrc.Float64() * 2 * math.Pi
			dist := searchRadius * randSrc.Float64()
			center := image.Point{
				X: anchor.X + int(dist*math.Cos(angle)),
				Y: anchor.Y + int(dist*math.Sin(angle)),
			}
			// For fully random distribution, pick any point on the map
			if settings.BuildingDistribution == 100 {
				center = image.Point{
					X: randSrc.Intn(width),
					Y: randSrc.Intn(height),
				}
			}

			// Ensure the center point is within the map boundaries
			if center.X < 0 || center.Y < 0 || center.X >= width || center.Y >= height {
				continue
			}

			// Attempt to create a building at the selected center
			size := minBuildingSizePx + randSrc.Float64()*(maxBuildingSizePx-minBuildingSizePx)
			shape := settings.BuildingShape
			if shape == "mixed" {
				shape = chooseShape(randSrc, settings.BuildingShapeRatios)
			}

			var pixels []image.Point
			var ok bool

			if shape == "procedural" {
				pixels, ok = getProceduralBuildingPixels(center, size, settings, waterMask, roadMask, buildingMask, width, height, randSrc)
			} else {
				pixels, ok = getBuildingPixels(center, size, shape, waterMask, roadMask, buildingMask, width, height, randSrc)
			}

			if ok {
				// If successful, draw the building and update data structures
				for _, p := range pixels {
					img.Set(p.X, p.Y, buildingColor)
					buildingMask.SetPoint(p)
				}
				buildings = append(buildings, pixels)
				buildingsPlaced++
				break // Move to the next building
			}
		}
	}
	return buildings, buildingMask
}

// getProceduralBuildingPixels generates a complex building by connecting multiple shapes.
func getProceduralBuildingPixels(center image.Point, size float64, settings *Settings, waterMask, roadMask, buildingMask *PixelMask, width, height int, randSrc *rand.Rand) ([]image.Point, bool) {
	complexity := settings.MinBuildingComplexity
	if settings.BuildingComplexityRatio > randSrc.Float64()*100 {
		complexity = settings.MinBuildingComplexity + randSrc.Intn(settings.MaxBuildingComplexity-settings.MinBuildingComplexity+1)
	}

	type shapeDescription struct {
		shape  string
		center image.Point
		size   float64
	}

	var shapeDescriptions []shapeDescription
	var buildingCenter image.Point

	// Generate component shapes
	for i := 0; i < complexity; i++ {
		shape := chooseShape(randSrc, settings.BuildingShapeRatios)
		componentSize := size * (0.5 + randSrc.Float64()*0.5) // Components can be 50-100% of the building size

		var newCenter image.Point
		if i == 0 {
			newCenter = center
			buildingCenter = center
		} else {
			prevShape := shapeDescriptions[randSrc.Intn(len(shapeDescriptions))]
			angle := randSrc.Float64() * 2 * math.Pi
			dist := componentSize * (0.25 + randSrc.Float64()*0.5) // Overlap between 25% and 75%
			newCenter = image.Point{
				X: prevShape.center.X + int(dist*math.Cos(angle)),
				Y: prevShape.center.Y + int(dist*math.Sin(angle)),
			}
		}
		shapeDescriptions = append(shapeDescriptions, shapeDescription{shape, newCenter, componentSize})
	}

	var minX, minY, maxX, maxY int
	for i, sd := range shapeDescriptions {
		halfSize := int(sd.size / 2)
		if i == 0 {
			minX, minY = sd.center.X-halfSize, sd.center.Y-halfSize
			maxX, maxY = sd.center.X+halfSize, sd.center.Y+halfSize
		} else {
			if sd.center.X-halfSize < minX {
				minX = sd.center.X - halfSize
			}
			if sd.center.Y-halfSize < minY {
				minY = sd.center.Y - halfSize
			}
			if sd.center.X+halfSize > maxX {
				maxX = sd.center.X + halfSize
			}
			if sd.center.Y+halfSize > maxY {
				maxY = sd.center.Y + halfSize
			}
		}
	}

	// Calculate scaling factor
	currentWidth := float64(maxX - minX)
	currentHeight := float64(maxY - minY)
	scale := size / math.Max(currentWidth, currentHeight)

	// Generate final pixels
	var finalPixels []image.Point
	pixelMap := make(map[image.Point]bool)
	for _, sd := range shapeDescriptions {
		scaledSize := sd.size * scale
		scaledCenterX := buildingCenter.X + int((float64(sd.center.X)-float64(minX)-currentWidth/2)*scale)
		scaledCenterY := buildingCenter.Y + int((float64(sd.center.Y)-float64(minY)-currentHeight/2)*scale)
		pixels, ok := getComponentPixels(image.Point{X: scaledCenterX, Y: scaledCenterY}, scaledSize, sd.shape, randSrc)
		if !ok {
			continue
		}
		for _, p := range pixels {
			if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || waterMask.GetPoint(p) || roadMask.GetPoint(p) || buildingMask.GetPoint(p) {
				return nil, false
			}
			if !pixelMap[p] {
				finalPixels = append(finalPixels, p)
				pixelMap[p] = true
			}
		}
	}

	if len(finalPixels) == 0 {
		return nil, false
	}

	return finalPixels, true
}

// scalePixels scales the building to the final size.
func scalePixels(pixels []image.Point, finalSize float64) []image.Point {
	if len(pixels) == 0 {
		return pixels
	}

	minX, minY := pixels[0].X, pixels[0].Y
	maxX, maxY := pixels[0].X, pixels[0].Y
	for _, p := range pixels {
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

	// Calculate the current dimensions
	currentWidth := float64(maxX - minX)
	currentHeight := float64(maxY - minY)

	scale := finalSize / math.Max(currentWidth, currentHeight)

	// Calculate the center of the bounding box
	centerX := float64(minX) + currentWidth/2
	centerY := float64(minY) + currentHeight/2

	// Scale and translate the pixels
	var scaledPixels []image.Point
	pixelMap := make(map[image.Point]bool) // To avoid duplicate pixels
	for _, p := range pixels {
		// Translate to origin
		translatedX := float64(p.X) - centerX
		translatedY := float64(p.Y) - centerY

		// Scale
		scaledX := translatedX * scale
		scaledY := translatedY * scale

		// Translate back to the center
		finalX := int(math.Round(scaledX + centerX))
		finalY := int(math.Round(scaledY + centerY))

		newPoint := image.Point{X: finalX, Y: finalY}
		if !pixelMap[newPoint] {
			scaledPixels = append(scaledPixels, newPoint)
			pixelMap[newPoint] = true
		}
	}

	return scaledPixels
}

// getComponentPixels generates the pixels for a single shape component without collision checks.
func getComponentPixels(center image.Point, size float64, shape string, randSrc *rand.Rand) ([]image.Point, bool) {
	var pixels []image.Point
	var halfSize = int(size / 2)

	switch shape {
	case "squares":
		for y := center.Y - halfSize; y <= center.Y+halfSize; y++ {
			for x := center.X - halfSize; x <= center.X+halfSize; x++ {
				pixels = append(pixels, image.Point{X: x, Y: y})
			}
		}
	case "circles":
		r2 := (size / 2) * (size / 2)
		for y := center.Y - halfSize; y <= center.Y+halfSize; y++ {
			for x := center.X - halfSize; x <= center.X+halfSize; x++ {
				dx, dy := float64(x-center.X), float64(y-center.Y)
				if dx*dx+dy*dy <= r2 {
					pixels = append(pixels, image.Point{X: x, Y: y})
				}
			}
		}
	case "rectangles":
		longSide := size
		shortSide := randSrc.Float64()*(size-float64(halfSize)) + float64(halfSize)
		var w, h int
		if randSrc.Intn(2) == 0 {
			w, h = int(longSide), int(shortSide)
		} else {
			w, h = int(shortSide), int(longSide)
		}
		halfW, halfH := w/2, h/2
		for y := center.Y - halfH; y <= center.Y+halfH; y++ {
			for x := center.X - halfW; x <= center.X+halfW; x++ {
				pixels = append(pixels, image.Point{X: x, Y: y})
			}
		}
	}

	return pixels, len(pixels) > 0
}

// chooseShape selects a building shape based on the provided ratios.
func chooseShape(randSrc *rand.Rand, ratios map[string]float64) string {
	var shapes []string
	var weights []float64
	var cumulativeWeight float64
	for shape, weight := range ratios {
		shapes = append(shapes, shape)
		cumulativeWeight += weight
		weights = append(weights, cumulativeWeight)
	}

	// Generate a random number between 0 and the total weight
	randNum := randSrc.Float64() * cumulativeWeight

	for i, weight := range weights {
		if randNum < weight {
			return shapes[i]
		}
	}

	// Default to the first shape if something goes wrong
	return shapes[0]
}

// getBuildingPixels determines the pixels for a single building based on its shape and checks for collisions.
func getBuildingPixels(center image.Point, size float64, shape string, waterMask, roadMask, buildingMask *PixelMask, width, height int, randSrc *rand.Rand) ([]image.Point, bool) {
	var pixels []image.Point
	var halfSize = int(size / 2)

	// Generate pixels based on the selected building shape
	switch shape {
	case "squares":
		for y := center.Y - halfSize; y <= center.Y+halfSize; y++ {
			for x := center.X - halfSize; x <= center.X+halfSize; x++ {
				p := image.Point{X: x, Y: y}
				if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || waterMask.GetPoint(p) || roadMask.GetPoint(p) || buildingMask.GetPoint(p) {
					return nil, false // Collision detected
				}
				pixels = append(pixels, p)
			}
		}
	case "circles":
		r2 := (size / 2) * (size / 2)
		for y := center.Y - halfSize; y <= center.Y+halfSize; y++ {
			for x := center.X - halfSize; x <= center.X+halfSize; x++ {
				dx, dy := float64(x-center.X), float64(y-center.Y)
				if dx*dx+dy*dy <= r2 {
					p := image.Point{X: x, Y: y}
					if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || waterMask.GetPoint(p) || roadMask.GetPoint(p) || buildingMask.GetPoint(p) {
						return nil, false // Collision detected
					}
					pixels = append(pixels, p)
				}
			}
		}
	case "rectangles":
		longSide := size
		shortSide := randSrc.Float64()*(size-float64(halfSize)) + float64(halfSize)
		var w, h int
		if randSrc.Intn(2) == 0 {
			w, h = int(longSide), int(shortSide)
		} else {
			w, h = int(shortSide), int(longSide)
		}
		halfW, halfH := w/2, h/2

		for y := center.Y - halfH; y <= center.Y+halfH; y++ {
			for x := center.X - halfW; x <= center.X+halfW; x++ {
				p := image.Point{X: x, Y: y}
				if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || waterMask.GetPoint(p) || roadMask.GetPoint(p) || buildingMask.GetPoint(p) {
					return nil, false // Collision detected
				}
				pixels = append(pixels, p)
			}
		}
	}

	// Final check to ensure pixels were generated
	if len(pixels) == 0 {
		return nil, false
	}
	return pixels, true
}

// isPixelInSlice checks if a pixel is already in a slice of pixels.
func isPixelInSlice(pixel image.Point, pixelSlice []image.Point) bool {
	for _, p := range pixelSlice {
		if p == pixel {
			return true
		}
	}
	return false
}

var (
	u8BufferPool = sync.Pool{
		New: func() any { return make([]uint8, 0) },
	}
	f32BufferPool = sync.Pool{
		New: func() any { return make([]float32, 0) },
	}
)

func getU8Buffer(n int) []uint8 {
	buf := u8BufferPool.Get().([]uint8)
	if cap(buf) < n {
		return make([]uint8, n)
	}
	return buf[:n]
}

func putU8Buffer(buf []uint8) {
	if buf == nil {
		return
	}
	u8BufferPool.Put(buf[:0])
}

func getF32Buffer(n int) []float32 {
	buf := f32BufferPool.Get().([]float32)
	if cap(buf) < n {
		return make([]float32, n)
	}
	return buf[:n]
}

func putF32Buffer(buf []float32) {
	if buf == nil {
		return
	}
	f32BufferPool.Put(buf[:0])
}

func chamferDistanceFieldInto(baseMask []uint8, w, h int, dist []float32) []float32 {
	const maxF = 1e6
	total := w * h
	if len(dist) < total {
		dist = make([]float32, total)
	} else {
		dist = dist[:total]
	}

	for i := 0; i < total; i++ {
		if baseMask[i] == 1 {
			dist[i] = 0
		} else {
			dist[i] = maxF
		}
	}

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

// FlattenBuildingAreas flattens the terrain under buildings and blends the surrounding area.
func FlattenBuildingAreas(heightMap *image.RGBA, buildings [][]image.Point, width, height int) *image.RGBA {
	if len(buildings) == 0 {
		return heightMap
	}

	newHeightMap := image.NewRGBA(heightMap.Bounds())
	copy(newHeightMap.Pix, heightMap.Pix)
	const bufferRadius = 5.0

	for _, building := range buildings {
		if len(building) == 0 {
			continue
		}

		minX, minY := width-1, height-1
		maxX, maxY := 0, 0
		var totalGray uint32
		for _, p := range building {
			if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height {
				continue
			}
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
			srcIdx := p.Y*heightMap.Stride + p.X*4
			totalGray += uint32(heightMap.Pix[srcIdx])
		}
		if minX > maxX || minY > maxY {
			continue
		}
		avgGray := uint8(totalGray / uint32(len(building)))

		pad := int(bufferRadius)
		bx0 := max(0, minX-pad)
		by0 := max(0, minY-pad)
		bx1 := min(width-1, maxX+pad)
		by1 := min(height-1, maxY+pad)
		bw := bx1 - bx0 + 1
		bh := by1 - by0 + 1
		if bw <= 0 || bh <= 0 {
			continue
		}

		maskSize := bw * bh
		baseMask := getU8Buffer(maskSize)
		for i := range baseMask {
			baseMask[i] = 0
		}
		for _, p := range building {
			if p.X < bx0 || p.X > bx1 || p.Y < by0 || p.Y > by1 {
				continue
			}
			localIdx := (p.Y-by0)*bw + (p.X - bx0)
			baseMask[localIdx] = 1
		}

		distBuf := getF32Buffer(maskSize)
		dist := chamferDistanceFieldInto(baseMask, bw, bh, distBuf)

		for y := by0; y <= by1; y++ {
			localRow := (y - by0) * bw
			rowOffset := y * newHeightMap.Stride
			srcRowOffset := y * heightMap.Stride
			for x := bx0; x <= bx1; x++ {
				localIdx := localRow + (x - bx0)
				idx := rowOffset + x*4
				if baseMask[localIdx] == 1 {
					newHeightMap.Pix[idx] = avgGray
					newHeightMap.Pix[idx+1] = avgGray
					newHeightMap.Pix[idx+2] = avgGray
					newHeightMap.Pix[idx+3] = 255
					continue
				}

				d := float64(dist[localIdx])
				if d > bufferRadius {
					continue
				}
				blendFactor := d / bufferRadius
				if blendFactor > 1 {
					blendFactor = 1
				}
				srcIdx := srcRowOffset + x*4
				origGray := float64(heightMap.Pix[srcIdx])
				newGray := uint8(float64(avgGray)*(1.0-blendFactor) + origGray*blendFactor)
				newHeightMap.Pix[idx] = newGray
				newHeightMap.Pix[idx+1] = newGray
				newHeightMap.Pix[idx+2] = newGray
				newHeightMap.Pix[idx+3] = 255
			}
		}

		putF32Buffer(distBuf)
		putU8Buffer(baseMask)
	}

	return newHeightMap
}
