package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"sync"
	"unsafe"
)

// PointOfInterest represents a location on the map where roads may start, end, or intersect.
type PointOfInterest struct {
	X, Y        int
	Connections int
	IsExit      bool
}

// PathPoint represents a single point in a road's path, with a flag to indicate if it's a bridge.
type PathPoint struct {
	Point    image.Point
	IsBridge bool
}

// Road represents a connection between two Points of Interest.
type Road struct {
	Start, End *PointOfInterest
	Width      int
	Points     []PathPoint
	Importance int
}

// GenerateRoads is the main function for creating roads on the map.
func GenerateRoads(width, height int, settings *Settings, noiseImg image.Image, allWaterPixels []image.Point, seed int64) ([]image.Point, []image.Point, *image.RGBA) {
	// Step 1: Initialize a transparent image for drawing roads
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.Transparent)
		}
	}

	// Step 2: Set up random number generator and colors
	randSrc := rand.New(rand.NewSource(seed))
	roadColor := color.RGBA{R: 139, G: 69, B: 19, A: 255}
	bridgeColor := color.RGBA{R: 60, G: 42, B: 33, A: 255}

	// Step 3: Generate Points of Interest (POIs)
	pois := generatePOIs(width, height, settings, allWaterPixels, randSrc)
	if len(pois) == 0 {
		return nil, nil, img
	}

	// Step 4: Connect POIs to form roads
	roads := connectPOIs(pois, width, height, settings, randSrc, allWaterPixels)
	// Step 5: Assign widths to the roads based on their importance
	assignRoadWidths(roads, settings)

	// Step 6: Draw the roads on the image
	var allRoadPixels []image.Point
	var allBridgePixels []image.Point
	for _, road := range roads {
		roadPixels, bridgePixels := drawRoad(img, road.Points, roadColor, bridgeColor, road.Width)
		allRoadPixels = append(allRoadPixels, roadPixels...)
		allBridgePixels = append(allBridgePixels, bridgePixels...)
	}

	return allRoadPixels, allBridgePixels, img
}

// generatePOIs creates the initial set of points where roads will originate.
func generatePOIs(width, height int, settings *Settings, allWaterPixels []image.Point, randSrc *rand.Rand) []*PointOfInterest {
	numPOIs := settings.NumRoads / 2
	if numPOIs == 0 {
		return nil
	}

	waterMap := make(map[image.Point]bool)
	for _, p := range allWaterPixels {
		waterMap[p] = true
	}

	numExits := settings.RoadExits
	if numExits > settings.NumRoads {
		numExits = settings.NumRoads
	}

	pois := make([]*PointOfInterest, 0, numPOIs)
	centerX := width / 2
	centerY := height / 2

	// Distribution affects the radius of POI generation
	maxRadius := math.Min(float64(width)/2, float64(height)/2)
	radius := maxRadius * (settings.RoadDistribution / 100.0)

	for i := 0; i < numPOIs; i++ {
		var x, y int
		found := false
		for j := 0; j < 100; j++ { // Retries to find a land spot
			if i < numExits {
				// Create POIs at the map edges
				side := randSrc.Intn(4)
				switch side {
				case 0: // Top
					x = randSrc.Intn(width)
					y = 0
				case 1: // Bottom
					x = randSrc.Intn(width)
					y = height - 1
				case 2: // Left
					x = 0
					y = randSrc.Intn(height)
				case 3: // Right
					x = width - 1
					y = randSrc.Intn(height)
				}
			} else {
				// Create POIs within the map
				angle := randSrc.Float64() * 2 * math.Pi
				r := math.Sqrt(randSrc.Float64()) * radius
				x = int(float64(centerX) + r*math.Cos(angle))
				y = int(float64(centerY) + r*math.Sin(angle))
			}

			if !waterMap[image.Point{X: x, Y: y}] {
				found = true
				break
			}
		}
		if found {
			isExit := i < numExits
			pois = append(pois, &PointOfInterest{X: x, Y: y, IsExit: isExit})
		}
	}

	return pois
}

// connectPOIs creates roads by connecting the generated Points of Interest.
func connectPOIs(pois []*PointOfInterest, width, height int, settings *Settings, randSrc *rand.Rand, allWaterPixels []image.Point) []*Road {
	if len(pois) < 2 {
		return nil
	}

	var roads []*Road
	var roadChan = make(chan *Road)
	var wg sync.WaitGroup

	visited := make(map[*PointOfInterest]bool)
	existingRoads := make(map[string]bool)

	// Find the center-most POI to start connecting from
	centerX := width / 2
	centerY := height / 2
	var startNode *PointOfInterest
	minDist := -1.0

	for _, poi := range pois {
		if poi == nil {
			continue
		}
		dist := math.Sqrt(math.Pow(float64(poi.X-centerX), 2) + math.Pow(float64(poi.Y-centerY), 2))
		if startNode == nil || dist < minDist {
			minDist = dist
			startNode = poi
		}
	}

	if startNode == nil {
		return nil
	}

	visited[startNode] = true

	// Use average dimension for controlling road path calculation
	avgDim := float64(width+height) / 2.0
	numControlPoints := max(int(avgDim*0.03), 60)

	// Connect all POIs using a minimum spanning tree-like algorithm
	for len(visited) < len(pois) {
		var closest *PointOfInterest
		var fromNode *PointOfInterest
		minDist := -1.0

		for poi := range visited {
			for _, other := range pois {
				if poi == nil || other == nil {
					continue
				}
				if !visited[other] {
					dist := math.Sqrt(math.Pow(float64(poi.X-other.X), 2) + math.Pow(float64(poi.Y-other.Y), 2))

					// Check if a road already exists between these two POIs
					key := fmt.Sprintf("%p-%p", poi, other)
					if uintptr(unsafe.Pointer(poi)) > uintptr(unsafe.Pointer(other)) {
						key = fmt.Sprintf("%p-%p", other, poi)
					}
					if existingRoads[key] {
						continue
					}

					// Avoid connecting two exit points directly
					if poi.IsExit && other.IsExit {
						continue
					}

					if closest == nil || dist < minDist {
						minDist = dist
						closest = other
						fromNode = poi
					}
				}
			}
		}

		if closest != nil {
			visited[closest] = true
			fromNode.Connections++
			closest.Connections++

			// Add road to existing roads map to prevent duplicates
			key := fmt.Sprintf("%p-%p", fromNode, closest)
			if uintptr(unsafe.Pointer(fromNode)) > uintptr(unsafe.Pointer(closest)) {
				key = fmt.Sprintf("%p-%p", closest, fromNode)
			}
			existingRoads[key] = true
			wg.Add(1)
			go func(fromNode, closest *PointOfInterest) {
				defer wg.Done()
				localRand := rand.New(rand.NewSource(randSrc.Int63()))
				path := calculateRoadPath(fromNode, closest, settings.RoadCurvyness/100.0, avgDim, localRand, numControlPoints, allWaterPixels)
				roadChan <- &Road{
					Start:  fromNode,
					End:    closest,
					Points: path,
				}
			}(fromNode, closest)
		} else {
			// No more reachable POIs, break the loop
			break
		}
	}

	go func() {
		wg.Wait()
		close(roadChan)
	}()

	for road := range roadChan {
		roads = append(roads, road)
	}

	// Calculate road importance based on the number of connections at its endpoints
	for _, road := range roads {
		road.Importance = road.Start.Connections + road.End.Connections
	}

	return roads
}

// assignRoadWidths sets the width of each road based on its importance.
func assignRoadWidths(roads []*Road, settings *Settings) {
	if len(roads) == 0 {
		return
	}

	// Sort roads by importance in descending order
	sort.Slice(roads, func(i, j int) bool {
		return roads[i].Importance > roads[j].Importance
	})

	minWidth := settings.MinRoadWidth
	maxWidth := settings.MaxRoadWidth
	widthStep := 0.0
	if len(roads) > 1 {
		widthStep = (maxWidth - minWidth) / float64(len(roads)-1)
	}

	// Assign widths, with more important roads being wider
	for i, road := range roads {
		road.Width = int(maxWidth - float64(i)*widthStep)
	}
}

// drawRoad draws a single road on the image, including bridges.
func drawRoad(img *image.RGBA, points []PathPoint, roadColor, bridgeColor color.Color, width int) ([]image.Point, []image.Point) {
	var roadPixels []image.Point
	var bridgePixels []image.Point
	for i := 0; i < len(points)-1; i++ {
		p1 := points[i]
		p2 := points[i+1]
		c := roadColor
		isBridge := p1.IsBridge && p2.IsBridge
		if isBridge {
			c = bridgeColor
		}
		linePoints := drawLine(img, p1.Point.X, p1.Point.Y, p2.Point.X, p2.Point.Y, c, width)
		if isBridge {
			bridgePixels = append(bridgePixels, linePoints...)
		} else {
			roadPixels = append(roadPixels, linePoints...)
		}
	}
	return roadPixels, bridgePixels
}

// bresenhamRoad uses Bresenham's line algorithm to create a path between control points.
func bresenhamRoad(path []image.Point) []image.Point {
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

// calculateRoadPath computes the path for a road, including curves and bridges.
func calculateRoadPath(start, end *PointOfInterest, curvyness, avgDim float64, randSrc *rand.Rand, numControlPoints int, allWaterPixels []image.Point) []PathPoint {
	dx := end.X - start.X
	dy := end.Y - start.Y
	dist := math.Sqrt(float64(dx*dx + dy*dy))

	waterMap := make(map[image.Point]bool)
	for _, p := range allWaterPixels {
		waterMap[p] = true
	}

	if dist == 0 {
		return []PathPoint{{Point: image.Point{X: start.X, Y: start.Y}, IsBridge: waterMap[image.Point{X: start.X, Y: start.Y}]}}
	}

	// Adjust curviness based on the distance between the POIs
	distanceFactor := math.Min(1.0, dist/(avgDim*0.5))
	adjustedCurvyness := curvyness * distanceFactor

	if adjustedCurvyness == 0 {
		points := bresenhamRoad([]image.Point{{X: start.X, Y: start.Y}, {X: end.X, Y: end.Y}})
		pathPoints := make([]PathPoint, len(points))
		for i, p := range points {
			pathPoints[i] = PathPoint{Point: p, IsBridge: waterMap[p]}
		}
		return pathPoints
	}

	// Use sine waves to create curves in the road
	type wave struct {
		amplitude float64
		numWaves  float64
		phase     float64
	}

	waves := make([]wave, 2)
	amp := (avgDim / 10.0) * adjustedCurvyness
	mainWavelength := avgDim / 4.0
	if mainWavelength < 1 {
		mainWavelength = 1
	}
	baseNumWaves := (dist / mainWavelength) * adjustedCurvyness

	// Main wave for overall curve
	waves[0] = wave{
		amplitude: amp,
		numWaves:  baseNumWaves * (0.75 + randSrc.Float64()*0.5),
		phase:     randSrc.Float64() * 2 * math.Pi,
	}

	// Smaller wave for minor detours and a more natural look
	waves[1] = wave{
		amplitude: amp / 4,
		numWaves:  baseNumWaves * 4 * (0.75 + randSrc.Float64()*0.5),
		phase:     randSrc.Float64() * 2 * math.Pi,
	}

	// Generate control points for the curve
	controlPoints := make([]image.Point, numControlPoints+1)
	for i := 0; i <= numControlPoints; i++ {
		t := float64(i) / float64(numControlPoints)
		x := float64(start.X) + t*float64(dx)
		y := float64(start.Y) + t*float64(dy)

		p := image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
		if !waterMap[p] {
			perpX, perpY := -float64(dy)/dist, float64(dx)/dist

			totalOffset := 0.0
			for _, w := range waves {
				totalOffset += math.Sin(t*w.numWaves*2*math.Pi+w.phase) * w.amplitude
			}
			totalOffset *= math.Sin(t * math.Pi)

			x += totalOffset * perpX
			y += totalOffset * perpY
		}
		controlPoints[i] = image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
	}

	// Create the final path using Bresenham's algorithm between control points
	points := bresenhamRoad(controlPoints)
	pathPoints := make([]PathPoint, len(points))
	for i, p := range points {
		pathPoints[i] = PathPoint{Point: p, IsBridge: waterMap[p]}
	}
	return pathPoints
}

// drawLine draws a line with a specified width on the image.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.Color, width int) []image.Point {
	var points []image.Point
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy

	for {
		for i := -width / 2; i <= width/2; i++ {
			for j := -width / 2; j <= width/2; j++ {
				px := x0 + i
				py := y0 + j
				if img.Bounds().Min.X <= px && px < img.Bounds().Max.X && img.Bounds().Min.Y <= py && py < img.Bounds().Max.Y {
					img.Set(px, py, col)
					points = append(points, image.Point{X: px, Y: py})
				}
			}
		}

		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
	return points
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
