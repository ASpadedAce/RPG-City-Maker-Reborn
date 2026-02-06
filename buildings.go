package main

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
)

// GenerateBuildings creates and places buildings on the map.
func GenerateBuildings(img *image.RGBA, width, height int, settings *Settings, roadPixels, allWaterPixels []image.Point, seed int64) []image.Point {
	// Early exit if no buildings are to be generated
	if settings.NumBuildings == 0 {
		return nil
	}

	// Initialize random number generator
	randSrc := rand.New(rand.NewSource(seed))
	buildingColor := color.RGBA{R: 128, G: 128, B: 128, A: 255} // Gray color for buildings

	// Create lookup maps for water and road pixels for efficient collision detection
	isWater := make(map[image.Point]bool)
	for _, p := range allWaterPixels {
		isWater[p] = true
	}

	isRoad := make(map[image.Point]bool)
	for _, p := range roadPixels {
		isRoad[p] = true
	}

	// Initialize building data structures
	isBuilding := make(map[image.Point]bool)
	var buildingPixels []image.Point
	var anchorPoints []image.Point

	// Determine anchor points for building placement
	if len(roadPixels) > 0 {
		anchorPoints = roadPixels
	} else {
		// If no roads, use all land pixels as anchors
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := image.Point{X: x, Y: y}
				if !isWater[p] {
					anchorPoints = append(anchorPoints, p)
				}
			}
		}
	}

	// Early exit if no anchor points are available
	if len(anchorPoints) == 0 {
		return nil
	}
	// Sort anchor points for deterministic placement
	sort.Slice(anchorPoints, func(i, j int) bool {
		if anchorPoints[i].Y != anchorPoints[j].Y {
			return anchorPoints[i].Y < anchorPoints[j].Y
		}
		return anchorPoints[i].X < anchorPoints[j].X
	})

	// Collect all land points for random placement
	landPoints := make([]image.Point, 0, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			p := image.Point{X: x, Y: y}
			if !isWater[p] && !isRoad[p] {
				landPoints = append(landPoints, p)
			}
		}
	}

	// Main loop for placing buildings
	buildingsPlaced := 0
	searchTries := 100                                // Number of attempts to find a spot for a building around an anchor
	maxPlacementAttempts := settings.NumBuildings * 5 // To prevent infinite loops

	for buildingsPlaced < settings.NumBuildings && maxPlacementAttempts > 0 {
		maxPlacementAttempts--

		// Select an anchor point for the new building
		var anchor image.Point
		if randSrc.Float64() > settings.BuildingDistribution/100.0 {
			// Place near roads or other existing features
			anchor = anchorPoints[randSrc.Intn(len(anchorPoints))]
		} else {
			// Place randomly on any available land
			if len(landPoints) == 0 {
				continue // No land to place buildings on
			}
			anchor = landPoints[randSrc.Intn(len(landPoints))]
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
			size := settings.MinBuildingSize + randSrc.Float64()*(settings.MaxBuildingSize-settings.MinBuildingSize)
			shape := settings.BuildingShape
			if shape == "mixed" {
				shape = chooseShape(randSrc, settings.BuildingShapeRatios)
			}
			pixels, ok := getBuildingPixels(center, size, shape, isWater, isRoad, isBuilding, width, height, randSrc)
			if ok {
				// If successful, draw the building and update data structures
				for _, p := range pixels {
					img.Set(p.X, p.Y, buildingColor)
					isBuilding[p] = true
					buildingPixels = append(buildingPixels, p)
				}
				buildingsPlaced++
				break // Move to the next building
			}
		}
	}
	return buildingPixels
}

// chooseShape selects a building shape based on the provided ratios.
func chooseShape(randSrc *rand.Rand, ratios map[string]float64) string {
	// Create a slice of shapes and their cumulative weights
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

	// Find the shape corresponding to the random number
	for i, weight := range weights {
		if randNum < weight {
			return shapes[i]
		}
	}

	// Default to the first shape if something goes wrong
	return shapes[0]
}

// getBuildingPixels determines the pixels for a single building based on its shape and checks for collisions.
func getBuildingPixels(center image.Point, size float64, shape string, isWater, isRoad, isBuilding map[image.Point]bool, width, height int, randSrc *rand.Rand) ([]image.Point, bool) {
	var pixels []image.Point
	var halfSize = int(size / 2)

	// Generate pixels based on the selected building shape
	switch shape {
	case "squares":
		for y := center.Y - halfSize; y <= center.Y+halfSize; y++ {
			for x := center.X - halfSize; x <= center.X+halfSize; x++ {
				p := image.Point{X: x, Y: y}
				if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || isWater[p] || isRoad[p] || isBuilding[p] {
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
					if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || isWater[p] || isRoad[p] || isBuilding[p] {
						return nil, false // Collision detected
					}
					pixels = append(pixels, p)
				}
			}
		}
	case "rectangles":
		// Create rectangles with varied aspect ratios
		longSide := size
		shortSide := randSrc.Float64()*(size-float64(halfSize)) + float64(halfSize)
		var w, h int
		if randSrc.Intn(2) == 0 {
			w, h = int(longSide), int(shortSide)
		} else {
			w, h = int(shortSide), int(longSide)
		}
		halfW, halfH := w/2, h/2

		// Check for collisions and gather pixels
		for y := center.Y - halfH; y <= center.Y+halfH; y++ {
			for x := center.X - halfW; x <= center.X+halfW; x++ {
				p := image.Point{X: x, Y: y}
				if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height || isWater[p] || isRoad[p] || isBuilding[p] {
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
