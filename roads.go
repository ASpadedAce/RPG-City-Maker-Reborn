package main

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
)

// PointOfInterest represents a location where roads may start, end, or intersect.
type PointOfInterest struct {
	X, Y           int
	Connections    int
	TargetDegree   int
	IsExit         bool
	ArterialWeight float64
}

// PathPoint represents a single point in a road's path with bridge flag.
type PathPoint struct {
	Point    image.Point
	IsBridge bool
}

// Road represents a connection between two points of interest.
type Road struct {
	Start, End *PointOfInterest
	Width      int
	Points     []PathPoint
	Importance int
}

const (
	minRoadWidthPercent  = 0.1
	maxRoadWidthPercent  = 5.0
	roadWidthPercentStep = 0.1
)

func clampRoadWidthPercent(v float64) float64 {
	if v < minRoadWidthPercent {
		return minRoadWidthPercent
	}
	if v > maxRoadWidthPercent {
		return maxRoadWidthPercent
	}
	return v
}

func snapRoadWidthPercent(v float64) float64 {
	v = clampRoadWidthPercent(v)
	steps := math.Round((v - minRoadWidthPercent) / roadWidthPercentStep)
	return clampRoadWidthPercent(minRoadWidthPercent + steps*roadWidthPercentStep)
}

func normalizeRoadWidthPercentRange(minPercent, maxPercent float64) (float64, float64) {
	minPercent = snapRoadWidthPercent(minPercent)
	maxPercent = snapRoadWidthPercent(maxPercent)
	if minPercent > maxPercent {
		minPercent, maxPercent = maxPercent, minPercent
	}
	return minPercent, maxPercent
}

func getRoadWidthRangePixels(settings *Settings, width, height int) (float64, float64) {
	minPercent, maxPercent := normalizeRoadWidthPercentRange(settings.MinRoadWidth, settings.MaxRoadWidth)
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

// GenerateRoads creates roads on the map.
func GenerateRoads(
	img *image.RGBA,
	width,
	height int,
	settings *Settings,
	waterMask *PixelMask,
	seed int64,
) (*PixelMask, *PixelMask, *PixelMask, []image.Point) {
	roadMask, bridgeMask, exitRoadMask, roadAnchors, _ := GenerateRoadsWithPOIs(img, width, height, settings, waterMask, nil, nil, 0, false, seed)
	return roadMask, bridgeMask, exitRoadMask, roadAnchors
}

func PrepareRoadNodes(width, height int, settings *Settings, waterMask *PixelMask, seed int64) ([]*PointOfInterest, int, bool) {
	randSrc := rand.New(rand.NewSource(seed))

	if settings.NumBuildings == 0 {
		internalRoads := int(math.Round(clamp(settings.RoadDistribution, 0, 100)))
		exitRoads := max(0, settings.RoadExits)
		if internalRoads == 0 && exitRoads > 0 && settings.RoadDistribution <= 0 {
			return nil, 0, true
		}
		if internalRoads > 0 {
			roadTarget := internalRoads
			return generatePOIs(width, height, settings, waterMask, randSrc, roadTarget), roadTarget, false
		}
		return nil, 0, false
	}

	roadTarget := estimateRoadTarget(settings)
	return generatePOIs(width, height, settings, waterMask, randSrc, roadTarget), roadTarget, false
}

func GenerateRoadsWithPOIs(
	img *image.RGBA,
	width,
	height int,
	settings *Settings,
	waterMask *PixelMask,
	wallLayout *FortificationLayout,
	pois []*PointOfInterest,
	roadTarget int,
	edgeToEdgeOnly bool,
	seed int64,
) (*PixelMask, *PixelMask, *PixelMask, []image.Point, []*Road) {
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	randSrc := rand.New(rand.NewSource(seed))
	roadColor := color.RGBA{R: 139, G: 69, B: 19, A: 255}
	bridgeColor := color.RGBA{R: 60, G: 42, B: 33, A: 255}

	if len(pois) > 0 && wallLayout != nil && wallLayout.Mask != nil {
		nudgePOIsOutsideWalls(pois, wallLayout.Mask, waterMask, settings, width, height, randSrc)
	}

	// Edge-case mode: no buildings.
	if settings.NumBuildings == 0 && roadTarget == 0 && !edgeToEdgeOnly {
		internalRoads := int(math.Round(clamp(settings.RoadDistribution, 0, 100)))
		exitRoads := max(0, settings.RoadExits)
		if internalRoads == 0 && exitRoads == 0 {
			return NewPixelMask(width, height), NewPixelMask(width, height), NewPixelMask(width, height), nil, nil
		}
		if internalRoads > 0 {
			roadTarget = internalRoads
		} else if settings.RoadDistribution <= 0 && exitRoads > 0 {
			edgeToEdgeOnly = true
		}
	}

	var roads []*Road
	if edgeToEdgeOnly {
		roads = generateEdgeToEdgeExitRoads(max(0, settings.RoadExits), width, height, settings, randSrc, waterMask, wallLayout)
	} else {
		if roadTarget <= 0 {
			roadTarget = estimateRoadTarget(settings)
		}
		if pois == nil {
			pois = generatePOIs(width, height, settings, waterMask, randSrc, roadTarget)
		}
		if len(pois) < 2 {
			return NewPixelMask(width, height), NewPixelMask(width, height), NewPixelMask(width, height), nil, nil
		}
		roads = connectPOIs(pois, width, height, settings, randSrc, waterMask, wallLayout, roadTarget)
		roads = appendExitRoads(roads, pois, width, height, settings, randSrc, waterMask, wallLayout)
	}

	if len(roads) == 0 {
		return NewPixelMask(width, height), NewPixelMask(width, height), NewPixelMask(width, height), nil, nil
	}
	roads = applyWallCrossingRules(roads, wallLayout, waterMask, randSrc)
	if len(roads) == 0 {
		return NewPixelMask(width, height), NewPixelMask(width, height), NewPixelMask(width, height), nil, nil
	}
	roads = reduceRepeatedBridges(roads, waterMask, width, height, randSrc)
	if len(roads) == 0 {
		return NewPixelMask(width, height), NewPixelMask(width, height), NewPixelMask(width, height), nil, nil
	}
	roads = ensureRoadNetworkConnected(roads, settings, randSrc, waterMask, wallLayout, width, height)
	assignRoadWidths(roads, settings, randSrc, width, height, wallLayout)

	roadMask := NewPixelMask(width, height)
	bridgeMask := NewPixelMask(width, height)
	exitRoadMask := NewPixelMask(width, height)
	for _, road := range roads {
		drawRoadToMasks(img, road.Points, roadColor, bridgeColor, road.Width, roadMask, bridgeMask)
		if road.Start.IsExit || road.End.IsExit {
			drawRoadToMasks(img, road.Points, roadColor, bridgeColor, road.Width, exitRoadMask, exitRoadMask)
		}
	}

	roadAnchors := roadMask.ToPoints()
	return roadMask, bridgeMask, exitRoadMask, roadAnchors, roads
}

func nudgePOIsOutsideWalls(pois []*PointOfInterest, wallMask, waterMask *PixelMask, settings *Settings, width, height int, randSrc *rand.Rand) {
	if len(pois) == 0 || wallMask == nil {
		return
	}
	if waterMask == nil {
		waterMask = NewPixelMask(width, height)
	}
	minWallPx, maxWallPx := getWallWidthRangePixels(settings, width, height)
	centerX := float64(width-1) * 0.5
	centerY := float64(height-1) * 0.5

	for _, p := range pois {
		if p == nil || !wallMask.GetXY(p.X, p.Y) {
			continue
		}

		wallWidthPx := minWallPx
		if maxWallPx > minWallPx {
			wallWidthPx = minWallPx + randSrc.Float64()*(maxWallPx-minWallPx)
		}
		nudgeFactor := 0.02 + randSrc.Float64()*0.03
		nudgeDist := int(math.Round(wallWidthPx * nudgeFactor))
		if nudgeDist < 1 {
			nudgeDist = 1
		}

		vx := float64(p.X) - centerX
		vy := float64(p.Y) - centerY
		vlen := math.Hypot(vx, vy)
		if vlen < 0.001 {
			theta := randSrc.Float64() * 2 * math.Pi
			vx = math.Cos(theta)
			vy = math.Sin(theta)
			vlen = 1
		}
		dx := vx / vlen
		dy := vy / vlen

		moved := false
		for step := 1; step <= nudgeDist+32; step++ {
			nx := int(math.Round(float64(p.X) + float64(step)*dx))
			ny := int(math.Round(float64(p.Y) + float64(step)*dy))
			if nx < 0 || ny < 0 || nx >= width || ny >= height {
				break
			}
			if wallMask.GetXY(nx, ny) || waterMask.GetXY(nx, ny) {
				continue
			}
			p.X = nx
			p.Y = ny
			moved = true
			break
		}
		if moved {
			continue
		}

		// Fallback: small radial sweep if direct outward ray was blocked.
		baseAngle := math.Atan2(dy, dx)
		for a := -6; a <= 6; a++ {
			ang := baseAngle + float64(a)*math.Pi/18.0
			adx := math.Cos(ang)
			ady := math.Sin(ang)
			for step := 1; step <= nudgeDist+32; step++ {
				nx := int(math.Round(float64(p.X) + float64(step)*adx))
				ny := int(math.Round(float64(p.Y) + float64(step)*ady))
				if nx < 0 || ny < 0 || nx >= width || ny >= height {
					break
				}
				if wallMask.GetXY(nx, ny) || waterMask.GetXY(nx, ny) {
					continue
				}
				p.X = nx
				p.Y = ny
				moved = true
				break
			}
			if moved {
				break
			}
		}
	}
}

func generateEdgeToEdgeExitRoads(exitRoads, width, height int, settings *Settings, randSrc *rand.Rand, waterMask *PixelMask, wallLayout *FortificationLayout) []*Road {
	if exitRoads <= 0 {
		return nil
	}
	avgDim := float64(width+height) / 2.0
	roads := make([]*Road, 0, exitRoads)
	for i := 0; i < exitRoads; i++ {
		start, end := sampleDifferentEdgePair(width, height, randSrc)
		start.IsExit = true
		end.IsExit = true
		path := calculateRoadPath(start, end, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMask, wallLayout)
		roads = append(roads, &Road{
			Start:      start,
			End:        end,
			Points:     path,
			Importance: 1,
		})
	}
	return roads
}

func sampleDifferentEdgePair(width, height int, randSrc *rand.Rand) (*PointOfInterest, *PointOfInterest) {
	sideA := randSrc.Intn(4)
	sideB := randSrc.Intn(3)
	if sideB >= sideA {
		sideB++
	}
	return sampleEdgePOIBySide(width, height, sideA, randSrc), sampleEdgePOIBySide(width, height, sideB, randSrc)
}

func sampleEdgePOIBySide(width, height, side int, randSrc *rand.Rand) *PointOfInterest {
	switch side {
	case 0:
		return &PointOfInterest{X: randSrc.Intn(width), Y: 0}
	case 1:
		return &PointOfInterest{X: randSrc.Intn(width), Y: height - 1}
	case 2:
		return &PointOfInterest{X: 0, Y: randSrc.Intn(height)}
	default:
		return &PointOfInterest{X: width - 1, Y: randSrc.Intn(height)}
	}
}

func generatePOIs(width, height int, settings *Settings, waterMask *PixelMask, randSrc *rand.Rand, roadTarget int) []*PointOfInterest {
	distribution := clamp01(settings.RoadDistribution / 100.0)
	targetCoverage := 0.10 + 0.90*distribution
	minBuildingSizePx, maxBuildingSizePx := getBuildingSizeRangePixels(settings, width, height)
	avgBuildingSize := (minBuildingSizePx + maxBuildingSizePx) / 2.0
	if avgBuildingSize < 1 {
		avgBuildingSize = 1
	}

	coreNodes := estimateCoreNodeCount(width, height, distribution, avgBuildingSize, settings.NumBuildings)
	if coreNodes < 2 {
		coreNodes = 2
	}
	// Keep node count compatible with the requested road segment budget so a connected graph is feasible.
	maxTotalNodes := max(2, roadTarget+1)
	if coreNodes > maxTotalNodes {
		coreNodes = maxTotalNodes
	}

	centerX := width / 2
	centerY := height / 2
	effectiveRadius := math.Sqrt(targetCoverage) * (math.Min(float64(width), float64(height)) * 0.5)
	warpPhaseA := randSrc.Float64() * 2 * math.Pi
	warpPhaseB := randSrc.Float64() * 2 * math.Pi

	pois := make([]*PointOfInterest, 0, coreNodes)
	for len(pois) < coreNodes {
		x, y, ok := sampleCorePOI(width, height, distribution, targetCoverage, warpPhaseA, warpPhaseB, randSrc)
		if !ok {
			break
		}
		p := image.Point{X: x, Y: y}
		// Keep larger spacing between intersections so buildings have room.
		if waterMask.GetPoint(p) || isTooCloseToExisting(pois, x, y, avgBuildingSize*1.1) {
			continue
		}
		pois = append(pois, &PointOfInterest{X: x, Y: y, TargetDegree: sampleTargetDegree(randSrc)})
	}

	if len(pois) == 0 {
		return nil
	}

	for _, poi := range pois {
		centerDist := math.Hypot(float64(poi.X-centerX), float64(poi.Y-centerY))
		centerFactor := 1.0 - clamp01(centerDist/(effectiveRadius+1))
		sizeFactor := clamp01((avgBuildingSize - 4.0) / 40.0)
		poi.ArterialWeight = clamp01(0.60*centerFactor + 0.40*sizeFactor)
	}

	return pois
}

func estimateCoreNodeCount(width, height int, distribution, avgBuildingSize float64, numBuildings int) int {
	targetArea := float64(width*height) * (0.10 + 0.90*distribution)
	spacing := avgBuildingSize * (1.4 - 0.5*distribution)
	if spacing < 6 {
		spacing = 6
	}
	byArea := int((targetArea / (spacing * spacing)) * 0.18)
	buildingPressure := int(math.Sqrt(float64(max(numBuildings, 1))) * (0.7 + distribution*0.9))
	nodes := byArea + buildingPressure
	if nodes < 8 {
		nodes = 8
	}
	maxNodes := int(clamp(float64(width*height)/50000.0, 80, 550))
	if nodes > maxNodes {
		nodes = maxNodes
	}
	return nodes
}

func sampleCorePOI(width, height int, distribution, targetCoverage, warpPhaseA, warpPhaseB float64, randSrc *rand.Rand) (int, int, bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	// At 100% distribution, allow POIs over the entire canvas.
	if distribution >= 0.999 {
		return randSrc.Intn(width), randSrc.Intn(height), true
	}

	coverageRadius := math.Sqrt(clamp(targetCoverage, 0.01, 1.0))
	// Morph from round to squarer footprint as distribution rises.
	superellipsePower := 2.0 + 10.0*distribution
	warpAmp := (1.0 - distribution) * 0.18

	cx := float64(width-1) * 0.5
	cy := float64(height-1) * 0.5
	invHalfW := 1.0 / math.Max(float64(width-1)*0.5, 1.0)
	invHalfH := 1.0 / math.Max(float64(height-1)*0.5, 1.0)

	for i := 0; i < 120; i++ {
		x := randSrc.Intn(width)
		y := randSrc.Intn(height)
		nx := (float64(x) - cx) * invHalfW
		ny := (float64(y) - cy) * invHalfH

		ax := math.Abs(nx)
		ay := math.Abs(ny)
		metric := math.Pow(ax, superellipsePower) + math.Pow(ay, superellipsePower)
		theta := math.Atan2(ny, nx)
		warp := 1.0 + warpAmp*(0.55*math.Sin(3.0*theta+warpPhaseA)+0.45*math.Sin(5.0*theta+warpPhaseB))
		if warp < 0.7 {
			warp = 0.7
		}
		threshold := math.Pow(coverageRadius*warp, superellipsePower)
		if metric <= threshold {
			return x, y, true
		}
	}
	return 0, 0, false
}

func isTooCloseToExisting(pois []*PointOfInterest, x, y int, minDist float64) bool {
	minDist2 := minDist * minDist
	for _, p := range pois {
		dx := float64(p.X - x)
		dy := float64(p.Y - y)
		if dx*dx+dy*dy < minDist2 {
			return true
		}
	}
	return false
}

func sampleEdgePOI(width, height int, randSrc *rand.Rand) *PointOfInterest {
	side := randSrc.Intn(4)
	switch side {
	case 0:
		return &PointOfInterest{X: randSrc.Intn(width), Y: 0}
	case 1:
		return &PointOfInterest{X: randSrc.Intn(width), Y: height - 1}
	case 2:
		return &PointOfInterest{X: 0, Y: randSrc.Intn(height)}
	default:
		return &PointOfInterest{X: width - 1, Y: randSrc.Intn(height)}
	}
}

func sampleTargetDegree(randSrc *rand.Rand) int {
	r := randSrc.Float64()
	switch {
	case r < 0.03:
		return 1
	case r < 0.17:
		return 2
	case r < 0.40:
		return 3
	case r < 0.85:
		return 4
	default:
		return 5
	}
}

func connectPOIs(pois []*PointOfInterest, width, height int, settings *Settings, randSrc *rand.Rand, waterMask *PixelMask, wallLayout *FortificationLayout, roadTarget int) []*Road {
	minAngle := settings.MinRoadAngle * math.Pi / 180.0
	if minAngle < 0 {
		minAngle = 0
	}

	edgeDist := math.Min(float64(width), float64(height)) * 0.30
	if roadTarget < len(pois)-1 {
		roadTarget = len(pois) - 1
	}

	type edgeCandidate struct {
		a, b  int
		score float64
	}

	candidates := make([]edgeCandidate, 0, len(pois)*6)
	for i := 0; i < len(pois); i++ {
		for j := i + 1; j < len(pois); j++ {
			a := pois[i]
			b := pois[j]
			if a.IsExit && b.IsExit {
				continue
			}
			dx := float64(a.X - b.X)
			dy := float64(a.Y - b.Y)
			d := math.Hypot(dx, dy)
			if !a.IsExit && !b.IsExit && d > edgeDist {
				continue
			}
			if (a.IsExit || b.IsExit) && d > edgeDist*1.6 {
				continue
			}

			arterialBias := 1.0 - math.Abs(a.ArterialWeight-b.ArterialWeight)
			distanceBias := 1.0 - clamp01(d/(edgeDist*1.6))
			score := arterialBias*0.65 + distanceBias*0.35 + randSrc.Float64()*0.08
			candidates = append(candidates, edgeCandidate{a: i, b: j, score: score})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	selected := make(map[uint64]bool, roadTarget)
	adjAngles := make([][]float64, len(pois))
	selectedEdges := make([]edgeCandidate, 0, roadTarget)

	addEdge := func(pick edgeCandidate) {
		key := edgeKey(pick.a, pick.b)
		selected[key] = true
		selectedEdges = append(selectedEdges, pick)
		a := pois[pick.a]
		b := pois[pick.b]
		angAB := math.Atan2(float64(b.Y-a.Y), float64(b.X-a.X))
		angBA := normalizeAngle(angAB + math.Pi)
		a.Connections++
		b.Connections++
		adjAngles[pick.a] = append(adjAngles[pick.a], angAB)
		adjAngles[pick.b] = append(adjAngles[pick.b], angBA)
	}

	canUseEdge := func(pick edgeCandidate) bool {
		key := edgeKey(pick.a, pick.b)
		if selected[key] {
			return false
		}
		a := pois[pick.a]
		b := pois[pick.b]
		if a.Connections >= max(1, a.TargetDegree+1) || b.Connections >= max(1, b.TargetDegree+1) {
			return false
		}
		angAB := math.Atan2(float64(b.Y-a.Y), float64(b.X-a.X))
		angBA := normalizeAngle(angAB + math.Pi)
		if !angleAllowed(adjAngles[pick.a], angAB, minAngle) || !angleAllowed(adjAngles[pick.b], angBA, minAngle) {
			return false
		}
		return pick.score-degreePenalty(a, b) >= -0.4
	}

	// Phase 1: enforce one connected backbone.
	start := 0
	bestWeight := pois[0].ArterialWeight
	for i := 1; i < len(pois); i++ {
		if pois[i].ArterialWeight > bestWeight {
			start = i
			bestWeight = pois[i].ArterialWeight
		}
	}
	connected := make([]bool, len(pois))
	connected[start] = true
	connectedCount := 1

	for connectedCount < len(pois) && len(selectedEdges) < roadTarget {
		bestIdx := -1
		bestScore := -1.0
		for idx, c := range candidates {
			aConn := connected[c.a]
			bConn := connected[c.b]
			if aConn == bConn {
				continue
			}
			if !canUseEdge(c) {
				continue
			}
			if c.score > bestScore {
				bestScore = c.score
				bestIdx = idx
			}
		}
		if bestIdx == -1 {
			break
		}
		pick := candidates[bestIdx]
		addEdge(pick)
		if !connected[pick.a] {
			connected[pick.a] = true
			connectedCount++
		}
		if !connected[pick.b] {
			connected[pick.b] = true
			connectedCount++
		}
	}

	// Phase 2: add extra links up to the target.
	for _, pick := range candidates {
		if len(selectedEdges) >= roadTarget {
			break
		}
		if !canUseEdge(pick) {
			continue
		}
		addEdge(pick)
	}

	roads := make([]*Road, 0, len(selectedEdges))
	avgDim := float64(width+height) / 2
	for _, e := range selectedEdges {
		a := pois[e.a]
		b := pois[e.b]
		path := calculateRoadPath(a, b, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMask, wallLayout)
		imp := a.Connections + b.Connections + int(math.Round((a.ArterialWeight+b.ArterialWeight)*4))
		roads = append(roads, &Road{Start: a, End: b, Points: path, Importance: imp})
	}

	return roads
}

func appendExitRoads(roads []*Road, pois []*PointOfInterest, width, height int, settings *Settings, randSrc *rand.Rand, waterMask *PixelMask, wallLayout *FortificationLayout) []*Road {
	if settings.RoadExits <= 0 || len(pois) == 0 {
		return roads
	}

	exitRoadsAdded := 0
	avgDim := float64(width+height) / 2
	usedEdgePoints := make([]image.Point, 0, settings.RoadExits)

	for i := 0; i < settings.RoadExits; i++ {
		edgeNode, ok := sampleNonWaterEdgePOI(width, height, randSrc, waterMask, usedEdgePoints)
		if !ok {
			continue
		}

		anchor := chooseExitAnchor(pois, usedEdgePoints, randSrc)
		if anchor == nil {
			continue
		}

		path := calculateRoadPath(anchor, edgeNode, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMask, wallLayout)
		if wallLayout != nil && wallLayout.Mask != nil && len(crossedWallIDs(path, wallLayout)) == 0 {
			bestScore := -1.0
			bestAnchor := anchor
			bestPath := path
			for _, cand := range pois {
				testPath := calculateRoadPath(cand, edgeNode, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMask, wallLayout)
				if len(crossedWallIDs(testPath, wallLayout)) == 0 {
					continue
				}
				d := math.Hypot(float64(cand.X-edgeNode.X), float64(cand.Y-edgeNode.Y))
				score := cand.ArterialWeight*2.0 + clamp(1.0-d/2000.0, 0, 1)
				if score > bestScore {
					bestScore = score
					bestAnchor = cand
					bestPath = testPath
				}
			}
			anchor = bestAnchor
			path = bestPath
		}
		if wallLayout != nil && wallLayout.Mask != nil && len(wallLayout.Coverages) > 0 {
			// Exit roads always use the gate-cheat when walls exist so they are always placeable.
			if forced, ok := forcePathThroughWallGate(anchor, edgeNode, wallLayout, waterMask); ok {
				path = forced
			}
		}

		anchor.Connections++
		edgeNode.IsExit = true
		edgeNode.TargetDegree = 1
		edgeNode.Connections = 1
		importance := anchor.Connections + edgeNode.Connections + int(math.Round(anchor.ArterialWeight*3))
		roads = append(roads, &Road{
			Start:      anchor,
			End:        edgeNode,
			Points:     path,
			Importance: importance,
		})
		usedEdgePoints = append(usedEdgePoints, image.Point{X: edgeNode.X, Y: edgeNode.Y})
		exitRoadsAdded++
	}

	_ = exitRoadsAdded
	return roads
}

func forcePathThroughWallGate(start, end *PointOfInterest, wallLayout *FortificationLayout, waterMask *PixelMask) ([]PathPoint, bool) {
	if start == nil || end == nil || wallLayout == nil || wallLayout.Mask == nil {
		return nil, false
	}
	mid, ok := nearestWallPixelToSegment(image.Point{X: start.X, Y: start.Y}, image.Point{X: end.X, Y: end.Y}, wallLayout.Mask)
	if !ok {
		return nil, false
	}
	tx, ty, ok := estimateWallTangent(mid, wallLayout.Mask)
	if !ok {
		return nil, false
	}
	nx, ny := -ty, tx
	rx := float64(end.X - start.X)
	ry := float64(end.Y - start.Y)
	if rx*nx+ry*ny < 0 {
		nx, ny = -nx, -ny
	}
	left, lok := walkToOutsideWall(mid, -nx, -ny, wallLayout.Mask)
	right, rok := walkToOutsideWall(mid, nx, ny, wallLayout.Mask)
	if !lok || !rok || left == right {
		return nil, false
	}

	startPt := image.Point{X: start.X, Y: start.Y}
	endPt := image.Point{X: end.X, Y: end.Y}
	entry, exit := left, right
	d1 := sqDist(startPt, left) + sqDist(endPt, right)
	d2 := sqDist(startPt, right) + sqDist(endPt, left)
	if d2 < d1 {
		entry, exit = right, left
	}

	seg1 := bresenhamRoad([]image.Point{startPt, entry})
	seg2 := bresenhamRoad([]image.Point{entry, exit})
	seg3 := bresenhamRoad([]image.Point{exit, endPt})
	out := make([]image.Point, 0, len(seg1)+len(seg2)+len(seg3))
	appendDedup := func(seg []image.Point) {
		for _, p := range seg {
			if len(out) > 0 && out[len(out)-1] == p {
				continue
			}
			out = append(out, p)
		}
	}
	appendDedup(seg1)
	appendDedup(seg2)
	appendDedup(seg3)
	return toPathPoints(out, waterMask), true
}

func nearestWallPixelToSegment(a, b image.Point, wallMask *PixelMask) (image.Point, bool) {
	if wallMask == nil || wallMask.Width <= 0 || wallMask.Height <= 0 {
		return image.Point{}, false
	}
	best := image.Point{}
	bestD2 := math.MaxFloat64
	found := false
	for y := 0; y < wallMask.Height; y++ {
		row := y * wallMask.Width
		for x := 0; x < wallMask.Width; x++ {
			if wallMask.Data[row+x] == 0 {
				continue
			}
			d2 := pointSegmentDistanceSquared(float64(x), float64(y), float64(a.X), float64(a.Y), float64(b.X), float64(b.Y))
			if d2 < bestD2 {
				bestD2 = d2
				best = image.Point{X: x, Y: y}
				found = true
			}
		}
	}
	return best, found
}

func pointSegmentDistanceSquared(px, py, ax, ay, bx, by float64) float64 {
	abx := bx - ax
	aby := by - ay
	apx := px - ax
	apy := py - ay
	den := abx*abx + aby*aby
	if den <= 1e-9 {
		dx := px - ax
		dy := py - ay
		return dx*dx + dy*dy
	}
	t := (apx*abx + apy*aby) / den
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	cx := ax + t*abx
	cy := ay + t*aby
	dx := px - cx
	dy := py - cy
	return dx*dx + dy*dy
}

func sampleNonWaterEdgePOI(width, height int, randSrc *rand.Rand, waterMask *PixelMask, used []image.Point) (*PointOfInterest, bool) {
	minSpacing := math.Min(float64(width), float64(height)) * 0.08
	minSpacing2 := minSpacing * minSpacing

	for tries := 0; tries < 120; tries++ {
		p := sampleEdgePOI(width, height, randSrc)
		pt := image.Point{X: p.X, Y: p.Y}
		if waterMask.GetPoint(pt) {
			continue
		}
		tooClose := false
		for _, u := range used {
			dx := float64(u.X - p.X)
			dy := float64(u.Y - p.Y)
			if dx*dx+dy*dy < minSpacing2 {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		return p, true
	}
	return nil, false
}

func chooseExitAnchor(pois []*PointOfInterest, usedExits []image.Point, randSrc *rand.Rand) *PointOfInterest {
	if len(pois) == 0 {
		return nil
	}
	if len(usedExits) == 0 {
		best := pois[0]
		for i := 1; i < len(pois); i++ {
			if pois[i].ArterialWeight > best.ArterialWeight {
				best = pois[i]
			}
		}
		return best
	}

	target := usedExits[len(usedExits)-1]
	best := pois[randSrc.Intn(len(pois))]
	bestScore := -1.0
	for _, p := range pois {
		d := math.Hypot(float64(p.X-target.X), float64(p.Y-target.Y))
		score := p.ArterialWeight*2.0 + clamp(1.0-d/2000.0, 0, 1)
		if score > bestScore {
			bestScore = score
			best = p
		}
	}
	return best
}

func estimateRoadTarget(settings *Settings) int {
	if settings.NumBuildings <= 0 {
		return 0
	}
	// Keep tiny settlements proportional: 1 building -> 1 road, etc.
	if settings.NumBuildings < 10 {
		return settings.NumBuildings
	}
	divisor := float64(max(settings.BuildingsPerRoad, 1))
	roads := int(math.Round(float64(max(settings.NumBuildings, 1)) / divisor))
	if roads < 1 {
		roads = 1
	}
	return roads
}

func edgeKey(a, b int) uint64 {
	if a > b {
		a, b = b, a
	}
	return (uint64(uint32(a)) << 32) | uint64(uint32(b))
}

func degreePenalty(a, b *PointOfInterest) float64 {
	penalty := 0.0
	if a.Connections >= a.TargetDegree {
		penalty += 0.20 + float64(a.Connections-a.TargetDegree)*0.12
	}
	if b.Connections >= b.TargetDegree {
		penalty += 0.20 + float64(b.Connections-b.TargetDegree)*0.12
	}
	return penalty
}

func angleAllowed(existing []float64, candidate, minAngle float64) bool {
	if minAngle <= 0 || len(existing) == 0 {
		return true
	}
	for _, ang := range existing {
		d := math.Abs(normalizeAngle(candidate - ang))
		if d > math.Pi {
			d = 2*math.Pi - d
		}
		if d < minAngle {
			return false
		}
	}
	return true
}

func normalizeAngle(a float64) float64 {
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	return a
}

func assignRoadWidths(roads []*Road, settings *Settings, randSrc *rand.Rand, width, height int, wallLayout *FortificationLayout) {
	if len(roads) == 0 {
		return
	}

	minWidth, maxWidth := getRoadWidthRangePixels(settings, width, height)
	if maxWidth < minWidth {
		minWidth, maxWidth = maxWidth, minWidth
	}

	maxImportance := 1
	for _, road := range roads {
		if road.Importance > maxImportance {
			maxImportance = road.Importance
		}
	}

	widths := make([]float64, len(roads))
	startNode := make([]int, len(roads))
	endNode := make([]int, len(roads))
	nodeIndex := make(map[*PointOfInterest]int, len(roads)*2)
	adj := make([][]int, 0, len(roads))
	getNodeID := func(p *PointOfInterest) int {
		if id, ok := nodeIndex[p]; ok {
			return id
		}
		id := len(adj)
		nodeIndex[p] = id
		adj = append(adj, nil)
		return id
	}

	for i, r := range roads {
		n := float64(r.Importance) / float64(maxImportance)
		jitter := (randSrc.Float64() - 0.5) * 0.16
		base := minWidth + (maxWidth-minWidth)*clamp01(n+jitter)
		widths[i] = base
		sid := getNodeID(r.Start)
		eid := getNodeID(r.End)
		startNode[i] = sid
		endNode[i] = eid
		adj[sid] = append(adj[sid], i)
		adj[eid] = append(adj[eid], i)
	}

	for i := 0; i < 2; i++ {
		next := make([]float64, len(widths))
		for ridx, w := range widths {
			total := w
			count := 1.0
			for _, nid := range []int{startNode[ridx], endNode[ridx]} {
				for _, nbr := range adj[nid] {
					if nbr == ridx {
						continue
					}
					total += widths[nbr]
					count += 1
				}
			}
			next[ridx] = w*0.55 + (total/count)*0.45
		}
		widths = next
	}

	for i, r := range roads {
		w := clamp(widths[i], minWidth, maxWidth)
		if wallLayout != nil && wallLayout.Mask != nil && len(crossedWallIDs(r.Points, wallLayout)) > 0 {
			// Wall-gate roads should be visibly substantial.
			minGateWidth := minWidth + 0.55*(maxWidth-minWidth)
			if w < minGateWidth {
				w = minGateWidth
			}
		}
		r.Width = max(1, int(math.Round(w)))
	}
}

// drawRoadToMasks draws a single road on the image including bridges.
func drawRoadToMasks(img *image.RGBA, points []PathPoint, roadColor, bridgeColor color.Color, width int, roadMask, bridgeMask *PixelMask) {
	bridgeWidth := int(math.Ceil(float64(width) * 1.15))
	if bridgeWidth < 1 {
		bridgeWidth = 1
	}

	for i := 0; i < len(points)-1; {
		p1 := points[i]
		p2 := points[i+1]
		isBridge := p1.IsBridge && p2.IsBridge
		if !isBridge {
			drawLineMasked(img, p1.Point.X, p1.Point.Y, p2.Point.X, p2.Point.Y, roadColor, width, roadMask)
			i++
			continue
		}

		// Draw each contiguous bridge run as one straight span.
		start := i
		end := i + 1
		for end < len(points)-1 && points[end].IsBridge && points[end+1].IsBridge {
			end++
		}
		drawLineMasked(
			img,
			points[start].Point.X, points[start].Point.Y,
			points[end].Point.X, points[end].Point.Y,
			bridgeColor,
			bridgeWidth,
			bridgeMask,
		)
		i = end
	}
}

func bresenhamRoad(path []image.Point) []image.Point {
	if len(path) < 2 {
		return path
	}

	fullPath := make([]image.Point, 0, len(path)*8)
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

// calculateRoadPath computes the path for a road including curves and bridges.
func calculateRoadPath(start, end *PointOfInterest, curvyness, avgDim float64, randSrc *rand.Rand, waterMask *PixelMask, wallLayout *FortificationLayout) []PathPoint {
	dx := end.X - start.X
	dy := end.Y - start.Y
	dist := math.Hypot(float64(dx), float64(dy))

	if dist == 0 {
		p := image.Point{X: start.X, Y: start.Y}
		return []PathPoint{{Point: p, IsBridge: waterMask.GetPoint(p)}}
	}

	curve := clamp(curvyness, 0, 1)
	if curve <= 0 {
		points := bresenhamRoad([]image.Point{{X: start.X, Y: start.Y}, {X: end.X, Y: end.Y}})
		return straightenPathAcrossWalls(toPathPoints(points, waterMask), wallLayout, waterMask)
	}

	// Non-linear scaling: low values stay fairly straight, high values become very winding.
	strength := math.Pow(curve, 1.35)
	if strength < 0.001 {
		points := bresenhamRoad([]image.Point{{X: start.X, Y: start.Y}, {X: end.X, Y: end.Y}})
		return straightenPathAcrossWalls(toPathPoints(points, waterMask), wallLayout, waterMask)
	}

	baseControls := int(math.Max(12, dist/(22.0-14.0*strength)))
	controlPoints := make([]image.Point, baseControls+1)
	perpX, perpY := -float64(dy)/dist, float64(dx)/dist
	lengthScale := clamp(dist/(avgDim*0.55), 0.45, 2.4)

	ampBase := clamp(dist*(0.01+0.13*strength*strength), 2, avgDim*0.16)
	amp1 := ampBase * (0.9 + randSrc.Float64()*0.25)
	amp2 := ampBase * (0.45 + randSrc.Float64()*0.20)
	amp3 := ampBase * (0.20 + randSrc.Float64()*0.15)

	w1 := clamp(dist*(1.10-0.70*strength), 30, avgDim*0.95)
	w2 := clamp(dist*(0.55-0.30*strength), 16, avgDim*0.55)
	w3 := clamp(dist*(0.26-0.12*strength), 8, avgDim*0.30)

	type wave struct {
		amplitude  float64
		wavelength float64
		phase      float64
	}

	waves := []wave{
		{
			amplitude:  amp1,
			wavelength: w1,
			phase:      randSrc.Float64() * 2 * math.Pi,
		},
		{
			amplitude:  amp2,
			wavelength: w2,
			phase:      randSrc.Float64() * 2 * math.Pi,
		},
		{
			amplitude:  amp3,
			wavelength: w3,
			phase:      randSrc.Float64() * 2 * math.Pi,
		},
	}

	for i := 0; i <= baseControls; i++ {
		t := float64(i) / float64(baseControls)
		x := float64(start.X) + t*float64(dx)
		y := float64(start.Y) + t*float64(dy)

		// Keep endpoints fixed while allowing large mid-segment deflection.
		envelope := math.Pow(math.Sin(t*math.Pi), 0.78)
		offset := 0.0
		for _, w := range waves {
			angle := (dist*t/w.wavelength)*2*math.Pi + w.phase
			offset += math.Sin(angle) * w.amplitude
		}
		offset *= envelope * lengthScale

		x += offset * perpX
		y += offset * perpY
		controlPoints[i] = image.Point{X: int(math.Round(x)), Y: int(math.Round(y))}
	}

	points := bresenhamRoad(controlPoints)
	return straightenPathAcrossWalls(toPathPoints(points, waterMask), wallLayout, waterMask)
}

func toPathPoints(points []image.Point, waterMask *PixelMask) []PathPoint {
	pathPoints := make([]PathPoint, len(points))
	for i, p := range points {
		isBridge := false
		if waterMask != nil {
			isBridge = waterMask.GetPoint(p)
		}
		pathPoints[i] = PathPoint{Point: p, IsBridge: isBridge}
	}
	return pathPoints
}

func wallIDAtPoint(p image.Point, wallLayout *FortificationLayout) int {
	if wallLayout == nil || wallLayout.Mask == nil {
		return 0
	}
	if !wallLayout.Mask.InBounds(p.X, p.Y) {
		return 0
	}
	if len(wallLayout.WallIDByPixel) != wallLayout.Mask.Width*wallLayout.Mask.Height {
		return 0
	}
	return wallLayout.WallIDByPixel[p.Y*wallLayout.Mask.Width+p.X]
}

func straightenPathAcrossWalls(points []PathPoint, wallLayout *FortificationLayout, waterMask *PixelMask) []PathPoint {
	if wallLayout == nil || wallLayout.Mask == nil || len(points) < 2 {
		return points
	}

	straight := make([]image.Point, 0, len(points))
	i := 0
	for i < len(points) {
		curr := points[i].Point
		currWallID := wallIDAtPoint(curr, wallLayout)
		if currWallID == 0 {
			straight = append(straight, curr)
			i++
			continue
		}

		start := i
		if start > 0 {
			start--
		}
		j := i
		for j < len(points) && wallIDAtPoint(points[j].Point, wallLayout) != 0 {
			j++
		}
		end := j
		if end >= len(points) {
			end = len(points) - 1
		}
		line := enforcePerpendicularWallCrossing(points, start, i, j, end, wallLayout)
		for k, p := range line {
			if len(straight) > 0 && k == 0 && straight[len(straight)-1] == p {
				continue
			}
			straight = append(straight, p)
		}
		i = j
	}

	return toPathPoints(straight, waterMask)
}

func enforcePerpendicularWallCrossing(points []PathPoint, start, wallStart, wallEnd, end int, wallLayout *FortificationLayout) []image.Point {
	startPt := points[start].Point
	endPt := points[end].Point
	baseLine := bresenhamRoad([]image.Point{startPt, endPt})
	if wallLayout == nil || wallLayout.Mask == nil {
		return baseLine
	}
	if wallStart < 0 || wallEnd <= wallStart || wallEnd > len(points) {
		return baseLine
	}

	mid := points[wallStart+(wallEnd-wallStart)/2].Point
	tx, ty, ok := estimateWallTangent(mid, wallLayout.Mask)
	if !ok {
		return baseLine
	}
	rx := float64(endPt.X - startPt.X)
	ry := float64(endPt.Y - startPt.Y)
	if crossingAngleToTangentDegrees(rx, ry, tx, ty) >= 75.0 {
		return baseLine
	}

	// Build a forced gate across the wall: one anchor just outside each side of the wall.
	nx, ny := -ty, tx
	vdot := rx*nx + ry*ny
	if vdot < 0 {
		nx, ny = -nx, -ny
	}
	left, lok := walkToOutsideWall(mid, -nx, -ny, wallLayout.Mask)
	right, rok := walkToOutsideWall(mid, nx, ny, wallLayout.Mask)
	if !lok || !rok || left == right {
		return baseLine
	}

	entry, exit := left, right
	d1 := sqDist(startPt, left) + sqDist(endPt, right)
	d2 := sqDist(startPt, right) + sqDist(endPt, left)
	if d2 < d1 {
		entry, exit = right, left
	}

	seg1 := bresenhamRoad([]image.Point{startPt, entry})
	seg2 := bresenhamRoad([]image.Point{entry, exit})
	seg3 := bresenhamRoad([]image.Point{exit, endPt})
	out := make([]image.Point, 0, len(seg1)+len(seg2)+len(seg3))
	appendDedup := func(seg []image.Point) {
		for _, p := range seg {
			if len(out) > 0 && out[len(out)-1] == p {
				continue
			}
			out = append(out, p)
		}
	}
	appendDedup(seg1)
	appendDedup(seg2)
	appendDedup(seg3)
	return out
}

func estimateWallTangent(mid image.Point, wallMask *PixelMask) (float64, float64, bool) {
	if wallMask == nil {
		return 0, 0, false
	}
	const r = 4
	var pts [][2]float64
	for dy := -r; dy <= r; dy++ {
		y := mid.Y + dy
		if y < 0 || y >= wallMask.Height {
			continue
		}
		for dx := -r; dx <= r; dx++ {
			x := mid.X + dx
			if x < 0 || x >= wallMask.Width {
				continue
			}
			if wallMask.GetXY(x, y) {
				pts = append(pts, [2]float64{float64(x), float64(y)})
			}
		}
	}
	if len(pts) < 3 {
		return 0, 0, false
	}

	var mx, my float64
	for _, p := range pts {
		mx += p[0]
		my += p[1]
	}
	mx /= float64(len(pts))
	my /= float64(len(pts))

	var sxx, syy, sxy float64
	for _, p := range pts {
		dx := p[0] - mx
		dy := p[1] - my
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	if sxx+syy < 0.001 {
		return 0, 0, false
	}
	theta := 0.5 * math.Atan2(2*sxy, sxx-syy)
	return math.Cos(theta), math.Sin(theta), true
}

func crossingAngleToTangentDegrees(rx, ry, tx, ty float64) float64 {
	rn := math.Hypot(rx, ry)
	tn := math.Hypot(tx, ty)
	if rn < 0.001 || tn < 0.001 {
		return 90
	}
	dot := (rx*tx + ry*ty) / (rn * tn)
	if dot < -1 {
		dot = -1
	}
	if dot > 1 {
		dot = 1
	}
	ang := math.Acos(math.Abs(dot)) * 180.0 / math.Pi
	return ang
}

func walkToOutsideWall(mid image.Point, dx, dy float64, wallMask *PixelMask) (image.Point, bool) {
	if wallMask == nil {
		return image.Point{}, false
	}
	maxSteps := max(8, (wallMask.Width+wallMask.Height)/12)
	for s := 1; s <= maxSteps; s++ {
		x := int(math.Round(float64(mid.X) + dx*float64(s)))
		y := int(math.Round(float64(mid.Y) + dy*float64(s)))
		if x < 0 || y < 0 || x >= wallMask.Width || y >= wallMask.Height {
			return image.Point{}, false
		}
		if !wallMask.GetXY(x, y) {
			return image.Point{X: x, Y: y}, true
		}
	}
	return image.Point{}, false
}

func sqDist(a, b image.Point) int {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return dx*dx + dy*dy
}

func crossedWallIDs(points []PathPoint, wallLayout *FortificationLayout) []int {
	if wallLayout == nil || wallLayout.Mask == nil || len(points) == 0 {
		return nil
	}
	seen := make(map[int]bool)
	out := make([]int, 0, 2)
	prevID := wallIDAtPoint(points[0].Point, wallLayout)
	for i := 1; i < len(points); i++ {
		currID := wallIDAtPoint(points[i].Point, wallLayout)
		if (prevID == 0 && currID > 0) || (prevID > 0 && currID == 0) {
			wid := currID
			if wid == 0 {
				wid = prevID
			}
			if wid > 0 && !seen[wid] {
				seen[wid] = true
				out = append(out, wid)
			}
		}
		prevID = currID
	}
	return out
}

func containsWallID(ids []int, wallID int) bool {
	for _, id := range ids {
		if id == wallID {
			return true
		}
	}
	return false
}

func applyWallCrossingRules(roads []*Road, wallLayout *FortificationLayout, waterMask *PixelMask, randSrc *rand.Rand) []*Road {
	if len(roads) == 0 || wallLayout == nil || wallLayout.Mask == nil || len(wallLayout.Coverages) == 0 {
		return roads
	}

	// Straighten each wall crossing segment first.
	for _, road := range roads {
		road.Points = straightenPathAcrossWalls(road.Points, wallLayout, waterMask)
	}

	type roadInfo struct {
		road *Road
		ids  []int
	}
	infos := make([]roadInfo, 0, len(roads))
	for _, road := range roads {
		infos = append(infos, roadInfo{road: road, ids: crossedWallIDs(road.Points, wallLayout)})
	}

	const repeatWallFactor = 0.55
	wallCrossCount := make(map[int]int)
	requiredWalls := make(map[int]bool)
	for i, cov := range wallLayout.Coverages {
		if cov < 95 {
			requiredWalls[i+1] = true
		}
	}

	keep := make([]bool, len(infos))
	for i, info := range infos {
		if len(info.ids) == 0 {
			keep[i] = true
			continue
		}
		if info.road.Start.IsExit || info.road.End.IsExit {
			keep[i] = true
			for _, wid := range info.ids {
				wallCrossCount[wid]++
			}
			continue
		}
		if crossesSameWallMultipleTimes(info.road.Points, wallLayout) {
			keep[i] = false
			continue
		}

		keepProb := 1.0
		for _, wid := range info.ids {
			c := wallCrossCount[wid]
			if c > 0 {
				keepProb *= math.Pow(repeatWallFactor, float64(c))
			}
		}
		if randSrc.Float64() <= keepProb {
			keep[i] = true
			for _, wid := range info.ids {
				wallCrossCount[wid]++
			}
		}
	}

	// Ensure at least one crossing on each wall unless its configured coverage is >= 95%.
	for wallID := range requiredWalls {
		if wallCrossCount[wallID] > 0 {
			continue
		}
		for i, info := range infos {
			if keep[i] {
				continue
			}
			if !containsWallID(info.ids, wallID) {
				continue
			}
			keep[i] = true
			for _, wid := range info.ids {
				wallCrossCount[wid]++
			}
			break
		}
	}

	filtered := make([]*Road, 0, len(roads))
	for i, info := range infos {
		if keep[i] {
			filtered = append(filtered, info.road)
		}
	}
	return filtered
}

func crossesSameWallMultipleTimes(points []PathPoint, wallLayout *FortificationLayout) bool {
	if wallLayout == nil || wallLayout.Mask == nil || len(points) < 2 {
		return false
	}
	transitionCount := make(map[int]int)
	prevID := wallIDAtPoint(points[0].Point, wallLayout)
	for i := 1; i < len(points); i++ {
		currID := wallIDAtPoint(points[i].Point, wallLayout)
		if (prevID == 0 && currID > 0) || (prevID > 0 && currID == 0) {
			wid := currID
			if wid == 0 {
				wid = prevID
			}
			if wid > 0 {
				transitionCount[wid]++
				// More than two transitions means re-crossing the same wall.
				if transitionCount[wid] > 2 {
					return true
				}
			}
		}
		prevID = currID
	}
	return false
}

func ensureRoadNetworkConnected(roads []*Road, settings *Settings, randSrc *rand.Rand, waterMask *PixelMask, wallLayout *FortificationLayout, width, height int) []*Road {
	if len(roads) <= 1 {
		return roads
	}

	avgDim := float64(width+height) / 2.0
	const maxConnectorAttempts = 32

	for attempts := 0; attempts < maxConnectorAttempts; attempts++ {
		nodeIndex := make(map[*PointOfInterest]int)
		nodes := make([]*PointOfInterest, 0, len(roads)*2)
		getNodeID := func(p *PointOfInterest) int {
			if id, ok := nodeIndex[p]; ok {
				return id
			}
			id := len(nodes)
			nodeIndex[p] = id
			nodes = append(nodes, p)
			return id
		}
		adj := make([][]int, 0, len(roads)*2)
		ensureAdj := func(n int) {
			for len(adj) <= n {
				adj = append(adj, nil)
			}
		}
		for _, r := range roads {
			a := getNodeID(r.Start)
			b := getNodeID(r.End)
			ensureAdj(a)
			ensureAdj(b)
			adj[a] = append(adj[a], b)
			adj[b] = append(adj[b], a)
		}

		compID := make([]int, len(nodes))
		for i := range compID {
			compID[i] = -1
		}
		compCount := 0
		queue := make([]int, 0, len(nodes))
		for i := 0; i < len(nodes); i++ {
			if compID[i] != -1 {
				continue
			}
			compID[i] = compCount
			queue = queue[:0]
			queue = append(queue, i)
			for h := 0; h < len(queue); h++ {
				cur := queue[h]
				for _, nb := range adj[cur] {
					if compID[nb] != -1 {
						continue
					}
					compID[nb] = compCount
					queue = append(queue, nb)
				}
			}
			compCount++
		}
		if compCount <= 1 {
			return roads
		}

		bestA, bestB := -1, -1
		bestDist2 := math.MaxFloat64
		for i := 0; i < len(nodes); i++ {
			for j := i + 1; j < len(nodes); j++ {
				if compID[i] == compID[j] {
					continue
				}
				dx := float64(nodes[i].X - nodes[j].X)
				dy := float64(nodes[i].Y - nodes[j].Y)
				d2 := dx*dx + dy*dy
				if d2 < bestDist2 {
					bestDist2 = d2
					bestA, bestB = i, j
				}
			}
		}
		if bestA == -1 || bestB == -1 {
			return roads
		}

		a := nodes[bestA]
		b := nodes[bestB]
		a.Connections++
		b.Connections++
		path := calculateRoadPath(a, b, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMask, wallLayout)
		roads = append(roads, &Road{
			Start:      a,
			End:        b,
			Points:     path,
			Importance: a.Connections + b.Connections + 2,
		})
	}

	return roads
}

// drawLineMasked draws a line with specified width on the image and mask.
func drawLineMasked(img *image.RGBA, x0, y0, x1, y1 int, col color.Color, width int, mask *PixelMask) {
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
					if mask != nil {
						mask.SetXY(px, py)
					}
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
}

func reduceRepeatedBridges(roads []*Road, waterMask *PixelMask, width, height int, randSrc *rand.Rand) []*Road {
	if len(roads) == 0 || waterMask == nil {
		return roads
	}

	regionByPixel := buildWaterRegionMap(waterMask)
	if len(regionByPixel) == 0 {
		return roads
	}

	// After first bridge on a water body, each additional bridge is progressively less likely.
	const repeatBridgeFactor = 0.45
	bodyBridgeCount := make(map[int]int)
	filtered := make([]*Road, 0, len(roads))

	for _, road := range roads {
		bridgedBodies := bridgedRegionIDs(road.Points, regionByPixel, width, height)
		if len(bridgedBodies) == 0 {
			filtered = append(filtered, road)
			continue
		}

		keepProb := 1.0
		for _, body := range bridgedBodies {
			c := bodyBridgeCount[body]
			if c > 0 {
				keepProb *= math.Pow(repeatBridgeFactor, float64(c))
			}
		}
		if randSrc.Float64() <= keepProb {
			filtered = append(filtered, road)
			for _, body := range bridgedBodies {
				bodyBridgeCount[body]++
			}
		}
	}

	return filtered
}

func buildWaterRegionMap(waterMask *PixelMask) []int {
	if waterMask == nil || waterMask.Width <= 0 || waterMask.Height <= 0 {
		return nil
	}
	total := waterMask.Width * waterMask.Height
	region := make([]int, total)
	nextRegionID := 1

	queue := make([]int, 0, 1024)
	for idx := 0; idx < total; idx++ {
		if waterMask.Data[idx] == 0 || region[idx] != 0 {
			continue
		}
		region[idx] = nextRegionID
		queue = queue[:0]
		queue = append(queue, idx)

		for head := 0; head < len(queue); head++ {
			cur := queue[head]
			x := cur % waterMask.Width
			y := cur / waterMask.Width

			neighbors := [][2]int{
				{x - 1, y}, {x + 1, y},
				{x, y - 1}, {x, y + 1},
			}
			for _, n := range neighbors {
				nx, ny := n[0], n[1]
				if nx < 0 || ny < 0 || nx >= waterMask.Width || ny >= waterMask.Height {
					continue
				}
				nidx := ny*waterMask.Width + nx
				if waterMask.Data[nidx] == 0 || region[nidx] != 0 {
					continue
				}
				region[nidx] = nextRegionID
				queue = append(queue, nidx)
			}
		}
		nextRegionID++
	}
	return region
}

func bridgedRegionIDs(points []PathPoint, regionByPixel []int, width, height int) []int {
	if len(points) == 0 || len(regionByPixel) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	seen := make(map[int]bool)
	out := make([]int, 0, 2)
	for _, pp := range points {
		if !pp.IsBridge {
			continue
		}
		x, y := pp.Point.X, pp.Point.Y
		if x < 0 || y < 0 || x >= width || y >= height {
			continue
		}
		rid := regionByPixel[y*width+x]
		if rid <= 0 || seen[rid] {
			continue
		}
		seen[rid] = true
		out = append(out, rid)
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
