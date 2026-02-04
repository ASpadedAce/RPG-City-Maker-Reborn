package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
	"unsafe"
)

type PointOfInterest struct {
	X, Y        int
	Connections int
}

type Road struct {
	Start, End, Control *PointOfInterest
	Width               int
	Points              []image.Point
	Importance          int
}

func GenerateRoads(width, height int, settings *Settings, noiseImg image.Image, allWaterPixels []image.Point) ([]image.Point, *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Transparent background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.Transparent)
		}
	}

	rand.Seed(settings.Seed)
	roadColor := color.RGBA{R: 139, G: 69, B: 19, A: 255}

	pois := generatePOIs(width, height, settings, allWaterPixels)
	if len(pois) == 0 {
		return nil, img
	}

	roads := connectPOIs(pois, width, height, settings)
	assignRoadWidths(roads, settings)

	var allRoadPixels []image.Point
	for _, road := range roads {
		roadPixels := drawCurve(img, road.Start, road.End, road.Control, roadColor, road.Width)
		allRoadPixels = append(allRoadPixels, roadPixels...)
	}

	return allRoadPixels, img
}

func generatePOIs(width, height int, settings *Settings, allWaterPixels []image.Point) []*PointOfInterest {
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

	// Distribution affects the radius
	maxRadius := math.Min(float64(width)/2, float64(height)/2)
	radius := maxRadius * (settings.RoadDistribution / 100.0)

	for i := 0; i < numPOIs; i++ {
		var x, y int
		found := false
		for j := 0; j < 100; j++ { // 100 retries to find a land spot
			if i < numExits {
				side := rand.Intn(4)
				switch side {
				case 0: // Top
					x = rand.Intn(width)
					y = 0
				case 1: // Bottom
					x = rand.Intn(width)
					y = height - 1
				case 2: // Left
					x = 0
					y = rand.Intn(height)
				case 3: // Right
					x = width - 1
					y = rand.Intn(height)
				}
			} else {
				angle := rand.Float64() * 2 * math.Pi
				r := rand.Float64() * radius
				x = int(float64(centerX) + r*math.Cos(angle))
				y = int(float64(centerY) + r*math.Sin(angle))
			}

			if !waterMap[image.Point{X: x, Y: y}] {
				found = true
				break
			}
		}
		if found {
			pois = append(pois, &PointOfInterest{X: x, Y: y})
		}
	}

	return pois
}

func connectPOIs(pois []*PointOfInterest, width, height int, settings *Settings) []*Road {
	if len(pois) < 2 {
		return nil
	}

	var roads []*Road
	visited := make(map[*PointOfInterest]bool)
	existingRoads := make(map[string]bool)

	// Find the center-most POI
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

					// Check if road exists
					key := fmt.Sprintf("%p-%p", poi, other)
					if uintptr(unsafe.Pointer(poi)) > uintptr(unsafe.Pointer(other)) {
						key = fmt.Sprintf("%p-%p", other, poi)
					}
					if existingRoads[key] {
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

			// Add road to existing roads map
			key := fmt.Sprintf("%p-%p", fromNode, closest)
			if uintptr(unsafe.Pointer(fromNode)) > uintptr(unsafe.Pointer(closest)) {
				key = fmt.Sprintf("%p-%p", closest, fromNode)
			}
			existingRoads[key] = true

			// Create control point for curve
			midX := (fromNode.X + closest.X) / 2
			midY := (fromNode.Y + closest.Y) / 2
			dist := math.Sqrt(math.Pow(float64(fromNode.X-closest.X), 2) + math.Pow(float64(fromNode.Y-closest.Y), 2))
			offset := dist * (settings.RoadCurvyness / 100.0) * (rand.Float64() - 0.5)

			controlX := int(float64(midX) + offset)
			controlY := int(float64(midY) + offset)

			roads = append(roads, &Road{
				Start:   fromNode,
				End:     closest,
				Control: &PointOfInterest{X: controlX, Y: controlY},
			})
		} else {
			// No more reachable POIs
			break
		}
	}

	for _, road := range roads {
		road.Importance = road.Start.Connections + road.End.Connections
	}

	return roads
}

func assignRoadWidths(roads []*Road, settings *Settings) {
	if len(roads) == 0 {
		return
	}

	sort.Slice(roads, func(i, j int) bool {
		return roads[i].Importance > roads[j].Importance
	})

	minWidth := settings.MinRoadWidth
	maxWidth := settings.MaxRoadWidth
	widthStep := 0.0
	if len(roads) > 1 {
		widthStep = (maxWidth - minWidth) / float64(len(roads)-1)
	}

	for i, road := range roads {
		road.Width = int(maxWidth - float64(i)*widthStep)
	}
}

func drawCurve(img *image.RGBA, p0, p1, p2 *PointOfInterest, col color.Color, width int) []image.Point {
	var points []image.Point
	var lastX, lastY int = -1, -1
	for t := 0.0; t <= 1.0; t += 0.01 {
		x := (1-t)*(1-t)*float64(p0.X) + 2*(1-t)*t*float64(p2.X) + t*t*float64(p1.X)
		y := (1-t)*(1-t)*float64(p0.Y) + 2*(1-t)*t*float64(p2.Y) + t*t*float64(p1.Y)

		if lastX != -1 {
			linePoints := drawLine(img, lastX, lastY, int(x), int(y), col, width)
			points = append(points, linePoints...)
		}
		lastX, lastY = int(x), int(y)
	}
	return points
}

// Bresenham's line algorithm for drawing segments of the curve
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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
