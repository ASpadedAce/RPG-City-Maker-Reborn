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

// GenerateRoads creates roads on the map.
func GenerateRoads(width, height int, settings *Settings, _ image.Image, allWaterPixels []image.Point, seed int64) ([]image.Point, []image.Point, *image.RGBA) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	randSrc := rand.New(rand.NewSource(seed))
	roadColor := color.RGBA{R: 139, G: 69, B: 19, A: 255}
	bridgeColor := color.RGBA{R: 60, G: 42, B: 33, A: 255}
	waterMap := make(map[image.Point]bool, len(allWaterPixels))
	for _, p := range allWaterPixels {
		waterMap[p] = true
	}

	roadTarget := estimateRoadTarget(settings, randSrc)
	pois := generatePOIs(width, height, settings, waterMap, randSrc, roadTarget)
	if len(pois) < 2 {
		return nil, nil, img
	}

	roads := connectPOIs(pois, width, height, settings, randSrc, waterMap, roadTarget)
	roads = appendExitRoads(roads, pois, width, height, settings, randSrc, waterMap)
	if len(roads) == 0 {
		return nil, nil, img
	}
	assignRoadWidths(roads, settings, randSrc)

	allRoadPixels := make([]image.Point, 0, len(roads)*64)
	allBridgePixels := make([]image.Point, 0, len(roads)*16)
	for _, road := range roads {
		roadPixels, bridgePixels := drawRoad(img, road.Points, roadColor, bridgeColor, road.Width)
		allRoadPixels = append(allRoadPixels, roadPixels...)
		allBridgePixels = append(allBridgePixels, bridgePixels...)
	}

	return allRoadPixels, allBridgePixels, img
}

func generatePOIs(width, height int, settings *Settings, waterMap map[image.Point]bool, randSrc *rand.Rand, roadTarget int) []*PointOfInterest {
	distribution := clamp01(settings.RoadDistribution / 100.0)
	avgBuildingSize := (settings.MinBuildingSize + settings.MaxBuildingSize) / 2.0
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
	maxRadius := math.Min(float64(width), float64(height)) * 0.48
	minRadius := math.Min(float64(width), float64(height)) * 0.10
	radius := minRadius + (maxRadius-minRadius)*distribution

	pois := make([]*PointOfInterest, 0, coreNodes)
	for len(pois) < coreNodes {
		x, y, ok := sampleCorePOI(centerX, centerY, radius, width, height, randSrc)
		if !ok {
			break
		}
		p := image.Point{X: x, Y: y}
		// Keep larger spacing between intersections so buildings have room.
		if waterMap[p] || isTooCloseToExisting(pois, x, y, avgBuildingSize*1.1) {
			continue
		}
		pois = append(pois, &PointOfInterest{X: x, Y: y, TargetDegree: sampleTargetDegree(randSrc)})
	}

	if len(pois) == 0 {
		return nil
	}

	for _, poi := range pois {
		centerDist := math.Hypot(float64(poi.X-centerX), float64(poi.Y-centerY))
		centerFactor := 1.0 - clamp01(centerDist/(radius+1))
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

func sampleCorePOI(centerX, centerY int, radius float64, width, height int, randSrc *rand.Rand) (int, int, bool) {
	for i := 0; i < 60; i++ {
		t := randSrc.Float64() * 2 * math.Pi
		r := radius * math.Sqrt(randSrc.Float64())
		x := centerX + int(math.Round(r*math.Cos(t)))
		y := centerY + int(math.Round(r*math.Sin(t)))
		if x >= 0 && x < width && y >= 0 && y < height {
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

func connectPOIs(pois []*PointOfInterest, width, height int, settings *Settings, randSrc *rand.Rand, waterMap map[image.Point]bool, roadTarget int) []*Road {
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
		path := calculateRoadPath(a, b, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMap)
		imp := a.Connections + b.Connections + int(math.Round((a.ArterialWeight+b.ArterialWeight)*4))
		roads = append(roads, &Road{Start: a, End: b, Points: path, Importance: imp})
	}

	return roads
}

func appendExitRoads(roads []*Road, pois []*PointOfInterest, width, height int, settings *Settings, randSrc *rand.Rand, waterMap map[image.Point]bool) []*Road {
	if settings.RoadExits <= 0 || len(pois) == 0 {
		return roads
	}

	exitRoadsAdded := 0
	avgDim := float64(width+height) / 2
	usedEdgePoints := make([]image.Point, 0, settings.RoadExits)

	for i := 0; i < settings.RoadExits; i++ {
		edgeNode, ok := sampleNonWaterEdgePOI(width, height, randSrc, waterMap, usedEdgePoints)
		if !ok {
			continue
		}

		anchor := chooseExitAnchor(pois, usedEdgePoints, randSrc)
		if anchor == nil {
			continue
		}

		anchor.Connections++
		edgeNode.IsExit = true
		edgeNode.TargetDegree = 1
		edgeNode.Connections = 1

		path := calculateRoadPath(anchor, edgeNode, settings.RoadCurvyness/100.0, avgDim, randSrc, waterMap)
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

func sampleNonWaterEdgePOI(width, height int, randSrc *rand.Rand, waterMap map[image.Point]bool, used []image.Point) (*PointOfInterest, bool) {
	minSpacing := math.Min(float64(width), float64(height)) * 0.08
	minSpacing2 := minSpacing * minSpacing

	for tries := 0; tries < 120; tries++ {
		p := sampleEdgePOI(width, height, randSrc)
		pt := image.Point{X: p.X, Y: p.Y}
		if waterMap[pt] {
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

func estimateRoadTarget(settings *Settings, randSrc *rand.Rand) int {
	// Two random numbers in [1,10], averaged -> triangular distribution centered at 10.5.
	divisor := float64((randSrc.Intn(10)+1)+(randSrc.Intn(10)+1)) / 2.0
	roads := int(math.Round(float64(max(settings.NumBuildings, 1)) / divisor))
	if roads < 4 {
		roads = 4
	}
	// Keep exits connectable and cap by graph size.
	if roads < settings.RoadExits {
		roads = settings.RoadExits
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

func assignRoadWidths(roads []*Road, settings *Settings, randSrc *rand.Rand) {
	if len(roads) == 0 {
		return
	}

	minWidth := settings.MinRoadWidth
	maxWidth := settings.MaxRoadWidth
	if maxWidth < minWidth {
		minWidth, maxWidth = maxWidth, minWidth
	}

	maxImportance := 1
	for _, road := range roads {
		if road.Importance > maxImportance {
			maxImportance = road.Importance
		}
	}

	widths := make(map[*Road]float64, len(roads))
	adj := make(map[*PointOfInterest][]*Road)
	for _, r := range roads {
		n := float64(r.Importance) / float64(maxImportance)
		jitter := (randSrc.Float64() - 0.5) * 0.16
		base := minWidth + (maxWidth-minWidth)*clamp01(n+jitter)
		widths[r] = base
		adj[r.Start] = append(adj[r.Start], r)
		adj[r.End] = append(adj[r.End], r)
	}

	for i := 0; i < 2; i++ {
		next := make(map[*Road]float64, len(widths))
		for r, w := range widths {
			total := w
			count := 1.0
			for _, n := range []*PointOfInterest{r.Start, r.End} {
				for _, nbr := range adj[n] {
					if nbr == r {
						continue
					}
					total += widths[nbr]
					count += 1
				}
			}
			next[r] = w*0.55 + (total/count)*0.45
		}
		widths = next
	}

	for _, r := range roads {
		w := clamp(widths[r], minWidth, maxWidth)
		r.Width = max(1, int(math.Round(w)))
	}
}

// drawRoad draws a single road on the image including bridges.
func drawRoad(img *image.RGBA, points []PathPoint, roadColor, bridgeColor color.Color, width int) ([]image.Point, []image.Point) {
	var roadPixels []image.Point
	var bridgePixels []image.Point
	bridgeWidth := int(math.Ceil(float64(width) * 1.15))
	if bridgeWidth < 1 {
		bridgeWidth = 1
	}

	for i := 0; i < len(points)-1; {
		p1 := points[i]
		p2 := points[i+1]
		isBridge := p1.IsBridge && p2.IsBridge
		if !isBridge {
			linePoints := drawLine(img, p1.Point.X, p1.Point.Y, p2.Point.X, p2.Point.Y, roadColor, width)
			roadPixels = append(roadPixels, linePoints...)
			i++
			continue
		}

		// Draw each contiguous bridge run as one straight span.
		start := i
		end := i + 1
		for end < len(points)-1 && points[end].IsBridge && points[end+1].IsBridge {
			end++
		}
		linePoints := drawLine(
			img,
			points[start].Point.X, points[start].Point.Y,
			points[end].Point.X, points[end].Point.Y,
			bridgeColor,
			bridgeWidth,
		)
		bridgePixels = append(bridgePixels, linePoints...)
		i = end
	}
	return roadPixels, bridgePixels
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
func calculateRoadPath(start, end *PointOfInterest, curvyness, avgDim float64, randSrc *rand.Rand, waterMap map[image.Point]bool) []PathPoint {
	dx := end.X - start.X
	dy := end.Y - start.Y
	dist := math.Hypot(float64(dx), float64(dy))

	if dist == 0 {
		p := image.Point{X: start.X, Y: start.Y}
		return []PathPoint{{Point: p, IsBridge: waterMap[p]}}
	}

	curve := clamp(curvyness, 0, 1)
	if curve <= 0 {
		points := bresenhamRoad([]image.Point{{X: start.X, Y: start.Y}, {X: end.X, Y: end.Y}})
		return toPathPoints(points, waterMap)
	}

	// Non-linear scaling: low values stay fairly straight, high values become very winding.
	strength := math.Pow(curve, 1.35)
	if strength < 0.001 {
		points := bresenhamRoad([]image.Point{{X: start.X, Y: start.Y}, {X: end.X, Y: end.Y}})
		return toPathPoints(points, waterMap)
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
	return toPathPoints(points, waterMap)
}

func toPathPoints(points []image.Point, waterMap map[image.Point]bool) []PathPoint {
	pathPoints := make([]PathPoint, len(points))
	for i, p := range points {
		pathPoints[i] = PathPoint{Point: p, IsBridge: waterMap[p]}
	}
	return pathPoints
}

// drawLine draws a line with specified width on the image.
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
