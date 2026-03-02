package main

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"sort"
)

const (
	minWallWidthPercent  = minBuildingSizePercent
	maxWallWidthPercent  = maxBuildingSizePercent
	wallWidthPercentStep = buildingSizePercentStep

	minTurretSizePercent  = 0.2
	maxTurretSizePercent  = maxWallWidthPercent
	turretSizePercentStep = 0.1
)

func clampWallWidthPercent(v float64) float64 {
	if v < minWallWidthPercent {
		return minWallWidthPercent
	}
	if v > maxWallWidthPercent {
		return maxWallWidthPercent
	}
	return v
}

func snapWallWidthPercent(v float64) float64 {
	v = clampWallWidthPercent(v)
	steps := math.Round((v - minWallWidthPercent) / wallWidthPercentStep)
	return clampWallWidthPercent(minWallWidthPercent + steps*wallWidthPercentStep)
}

func normalizeWallWidthPercentRange(minPercent, maxPercent float64) (float64, float64) {
	minPercent = snapWallWidthPercent(minPercent)
	maxPercent = snapWallWidthPercent(maxPercent)
	if minPercent > maxPercent {
		minPercent, maxPercent = maxPercent, minPercent
	}
	return minPercent, maxPercent
}

func getWallWidthRangePixels(settings *Settings, width, height int) (float64, float64) {
	minPercent, maxPercent := normalizeWallWidthPercentRange(settings.MinWallWidth, settings.MaxWallWidth)
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

func clampTurretSizePercent(v float64) float64 {
	if v < minTurretSizePercent {
		return minTurretSizePercent
	}
	if v > maxTurretSizePercent {
		return maxTurretSizePercent
	}
	return v
}

func snapTurretSizePercent(v float64) float64 {
	v = clampTurretSizePercent(v)
	steps := math.Round((v - minTurretSizePercent) / turretSizePercentStep)
	return clampTurretSizePercent(minTurretSizePercent + steps*turretSizePercentStep)
}

func getTurretSizePixels(settings *Settings, width, height int) float64 {
	sizePercent := snapTurretSizePercent(settings.TurretSize)
	avgDim := averageImageDimension(width, height)
	if avgDim < 1 {
		avgDim = 1
	}
	sizePx := (sizePercent / 100.0) * avgDim
	if sizePx < 1 {
		sizePx = 1
	}
	return sizePx
}

type FortificationLayout struct {
	Mask          *PixelMask
	WallIDByPixel []int
	Coverages     []float64
}

func GenerateFortifications(
	img *image.RGBA,
	width, height int,
	settings *Settings,
	waterMask *PixelMask,
	roadNodes []*PointOfInterest,
	seed int64,
) (*FortificationLayout, [][]image.Point) {
	layout := &FortificationLayout{
		Mask:          NewPixelMask(width, height),
		WallIDByPixel: make([]int, width*height),
	}
	if settings.NumWalls <= 0 || settings.CityCoverage <= 0 {
		return layout, nil
	}
	if waterMask == nil {
		waterMask = NewPixelMask(width, height)
	}
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, width, height))
	}

	randSrc := rand.New(rand.NewSource(seed))
	minWidthPx, maxWidthPx := getWallWidthRangePixels(settings, width, height)
	wallColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	walls := make([][]image.Point, 0, settings.NumWalls)
	outerCoverage := clamp(settings.CityCoverage, 1, 100)
	totalWalls := max(1, settings.NumWalls)
	prevCoverage := 101.0
	layout.Coverages = make([]float64, 0, totalWalls)

	for i := 0; i < totalWalls; i++ {
		baseCoverage := outerCoverage * float64(totalWalls-i) / float64(totalWalls)
		coverage := baseCoverage
		if i > 0 {
			coverage += randSrc.Float64()*10.0 - 5.0
		}
		coverage = clamp(coverage, 1, 100)
		if coverage >= prevCoverage {
			coverage = prevCoverage - 1
			if coverage < 1 {
				coverage = 1
			}
		}
		prevCoverage = coverage
		layout.Coverages = append(layout.Coverages, coverage)

		nodes := estimateWallNodeCount(coverage)
		wallPath := generateWallLoop(width, height, coverage, settings.WallCurvyness, nodes, randSrc, roadNodes)
		if len(wallPath) < 3 {
			continue
		}

		wallWidthPx := minWidthPx
		if maxWidthPx > minWidthPx {
			wallWidthPx = minWidthPx + randSrc.Float64()*(maxWidthPx-minWidthPx)
		}
		wallWidth := int(math.Round(wallWidthPx))
		if wallWidth < 1 {
			wallWidth = 1
		}

		pixels := drawWallLoopWithWaterGaps(img, wallPath, wallColor, wallWidth, layout.Mask, waterMask, layout.WallIDByPixel, i+1)
		if len(pixels) > 0 {
			walls = append(walls, pixels)
		}
	}

	return layout, walls
}

func estimateWallNodeCount(coverage float64) int {
	n := int(math.Round(20 + coverage*0.7))
	if n < 20 {
		n = 20
	}
	if n > 96 {
		n = 96
	}
	return n
}

func generateWallLoop(width, height int, coverage, curvyness float64, nodes int, randSrc *rand.Rand, roadNodes []*PointOfInterest) []image.Point {
	if width <= 0 || height <= 0 || nodes < 3 {
		return nil
	}

	centerX, centerY, baseRadiusX, baseRadiusY := wallEllipseFromRoadNodes(width, height, coverage, roadNodes)
	curveScale := clamp(curvyness, 0, 100) / 100.0
	warpAmp := 0.20 * curveScale
	phaseA := randSrc.Float64() * 2 * math.Pi
	phaseB := randSrc.Float64() * 2 * math.Pi

	out := make([]image.Point, 0, nodes+1)
	for i := 0; i < nodes; i++ {
		t := (2 * math.Pi * float64(i)) / float64(nodes)
		warp := 1.0 + warpAmp*(0.6*math.Sin(3*t+phaseA)+0.4*math.Sin(5*t+phaseB))
		if warp < 0.7 {
			warp = 0.7
		}
		rx := baseRadiusX * warp
		ry := baseRadiusY * warp
		x := int(math.Round(centerX + rx*math.Cos(t)))
		y := int(math.Round(centerY + ry*math.Sin(t)))
		if x < 0 {
			x = 0
		}
		if x >= width {
			x = width - 1
		}
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		out = append(out, image.Point{X: x, Y: y})
	}
	if len(out) > 0 {
		out = append(out, out[0])
	}
	return out
}

func wallEllipseFromRoadNodes(width, height int, coverage float64, roadNodes []*PointOfInterest) (centerX, centerY, radiusX, radiusY float64) {
	centerX = float64(width-1) * 0.5
	centerY = float64(height-1) * 0.5
	coverageRadius := math.Sqrt(clamp(coverage, 1, 100) / 100.0)
	radiusX = centerX * coverageRadius
	radiusY = centerY * coverageRadius

	if len(roadNodes) == 0 {
		return centerX, centerY, radiusX, radiusY
	}

	sumX, sumY := 0.0, 0.0
	for _, n := range roadNodes {
		sumX += float64(n.X)
		sumY += float64(n.Y)
	}
	centerX = sumX / float64(len(roadNodes))
	centerY = sumY / float64(len(roadNodes))

	dists := make([]float64, 0, len(roadNodes))
	var sx, sy float64
	for _, n := range roadNodes {
		dx := float64(n.X) - centerX
		dy := float64(n.Y) - centerY
		dists = append(dists, math.Hypot(dx, dy))
		sx += dx * dx
		sy += dy * dy
	}
	sort.Float64s(dists)
	q := clamp(coverage, 1, 100) / 100.0
	idx := int(math.Ceil(q*float64(len(dists)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(dists) {
		idx = len(dists) - 1
	}
	baseRadius := dists[idx]
	if baseRadius < 10 {
		baseRadius = 10
	}

	stdX := math.Sqrt(sx / float64(len(roadNodes)))
	stdY := math.Sqrt(sy / float64(len(roadNodes)))
	aspect := 1.0
	if stdY > 0.001 {
		aspect = stdX / stdY
	}
	aspect = clamp(aspect, 0.65, 1.55)
	radiusX = baseRadius * aspect
	radiusY = baseRadius / aspect

	maxRadiusX := math.Max(5, math.Min(centerX, float64(width-1)-centerX))
	maxRadiusY := math.Max(5, math.Min(centerY, float64(height-1)-centerY))
	radiusX = clamp(radiusX, 5, maxRadiusX)
	radiusY = clamp(radiusY, 5, maxRadiusY)

	return centerX, centerY, radiusX, radiusY
}

func drawWallLoopWithWaterGaps(
	img *image.RGBA,
	loop []image.Point,
	col color.RGBA,
	width int,
	wallMask *PixelMask,
	waterMask *PixelMask,
	wallIDByPixel []int,
	wallID int,
) []image.Point {
	if len(loop) < 2 || wallMask == nil {
		return nil
	}
	seen := make(map[int]bool)
	pixels := make([]image.Point, 0, len(loop)*8)
	radius := max(1, width/2)

	for i := 0; i < len(loop)-1; i++ {
		a := loop[i]
		b := loop[i+1]
		drawSegmentSelective(a.X, a.Y, b.X, b.Y, func(x, y int) {
			if !wallMask.InBounds(x, y) {
				return
			}
			if waterMask != nil && waterMask.GetXY(x, y) {
				return
			}
			for dy := -radius; dy <= radius; dy++ {
				yy := y + dy
				if yy < 0 || yy >= wallMask.Height {
					continue
				}
				for dx := -radius; dx <= radius; dx++ {
					if dx*dx+dy*dy > radius*radius {
						continue
					}
					xx := x + dx
					if xx < 0 || xx >= wallMask.Width {
						continue
					}
					if waterMask != nil && waterMask.GetXY(xx, yy) {
						continue
					}
					wallMask.SetXY(xx, yy)
					if len(wallIDByPixel) == wallMask.Width*wallMask.Height {
						wallIDByPixel[yy*wallMask.Width+xx] = wallID
					}
					img.Set(xx, yy, col)
					idx := yy*wallMask.Width + xx
					if !seen[idx] {
						seen[idx] = true
						pixels = append(pixels, image.Point{X: xx, Y: yy})
					}
				}
			}
		})
	}

	return pixels
}

func drawSegmentSelective(x0, y0, x1, y1 int, plot func(x, y int)) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy
	for {
		plot(x0, y0)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func cloneMask(src *PixelMask) *PixelMask {
	if src == nil {
		return nil
	}
	dst := NewPixelMask(src.Width, src.Height)
	copy(dst.Data, src.Data)
	return dst
}

func drawWallMask(img *image.RGBA, wallMask *PixelMask) {
	if img == nil || wallMask == nil {
		return
	}
	black := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	for y := 0; y < wallMask.Height; y++ {
		row := y * wallMask.Width
		for x := 0; x < wallMask.Width; x++ {
			if wallMask.Data[row+x] != 0 {
				img.Set(x, y, black)
			}
		}
	}
}

func GenerateTurrets(
	img *image.RGBA,
	width, height int,
	settings *Settings,
	layout *FortificationLayout,
	waterMask, roadMask *PixelMask,
	roads []*Road,
) *PixelMask {
	mask := NewPixelMask(width, height)
	if !settings.ShowTurrets || layout == nil || layout.Mask == nil || len(layout.WallIDByPixel) != width*height {
		return mask
	}
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	if waterMask == nil {
		waterMask = NewPixelMask(width, height)
	}
	if roadMask == nil {
		roadMask = NewPixelMask(width, height)
	}

	sizePx := getTurretSizePixels(settings, width, height)
	radius := int(math.Round(sizePx / 2.0))
	if radius < 1 {
		radius = 1
	}
	shape := settings.TurretShape
	if shape != "square" {
		shape = "circular"
	}
	colorRed := color.RGBA{R: 220, G: 25, B: 25, A: 255}

	wallPoints := make(map[int][]image.Point)
	waterMeetPoints := make(map[int][]image.Point)
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			wid := layout.WallIDByPixel[row+x]
			if wid <= 0 {
				continue
			}
			if !isBoundaryWallPixel(x, y, layout.Mask) {
				continue
			}
			p := image.Point{X: x, Y: y}
			wallPoints[wid] = append(wallPoints[wid], p)
			if touchesWater(x, y, waterMask) {
				waterMeetPoints[wid] = append(waterMeetPoints[wid], p)
			}
		}
	}

	occupied := make(map[int]bool)
	addTurret := func(center image.Point) {
		snapped, ok := snapPointToWall(center, layout.Mask, max(3, radius*4))
		if !ok {
			return
		}
		if nearbyTurretExists(mask, snapped, max(2, radius)) {
			return
		}
		key := snapped.Y*width + snapped.X
		if occupied[key] {
			return
		}
		occupied[key] = true
		drawTurret(img, mask, snapped, radius, shape, colorRed)
	}

	// Base spacing turrets along each wall ring.
	for wid, pts := range wallPoints {
		if len(pts) == 0 {
			continue
		}
		centroid := averagePoint(pts)
		sort.Slice(pts, func(i, j int) bool {
			ai := math.Atan2(float64(pts[i].Y-centroid.Y), float64(pts[i].X-centroid.X))
			aj := math.Atan2(float64(pts[j].Y-centroid.Y), float64(pts[j].X-centroid.X))
			return ai < aj
		})
		// Spacing is "distance along wall as % of wall circumference", independent of turret size.
		spacingPct := clamp(settings.TurretSpacing, 0, 100)
		step := int(math.Round((spacingPct / 100.0) * float64(len(pts))))
		if step < 1 {
			step = 1
		}
		if step > len(pts) {
			step = len(pts)
		}
		for i := 0; i < len(pts); i += step {
			addTurret(pts[i])
		}

		// Always place turrets where wall meets water.
		for _, p := range waterMeetPoints[wid] {
			addTurret(p)
		}
	}

	// Gate turrets: one on each side of each road crossing, spacing = 3x road width.
	for _, r := range roads {
		if r == nil || len(r.Points) < 2 {
			continue
		}
		gates := roadGateCentersForRoad(r, layout)
		if len(gates) == 0 {
			continue
		}
		for _, g := range gates {
			tx, ty, ok := fortEstimateWallTangent(g, layout.Mask)
			if !ok {
				continue
			}
			offset := 1.5 * float64(max(1, r.Width))
			left := image.Point{
				X: int(math.Round(float64(g.X) + tx*offset)),
				Y: int(math.Round(float64(g.Y) + ty*offset)),
			}
			right := image.Point{
				X: int(math.Round(float64(g.X) - tx*offset)),
				Y: int(math.Round(float64(g.Y) - ty*offset)),
			}
			addTurret(left)
			addTurret(right)
		}
	}

	return mask
}

func isBoundaryWallPixel(x, y int, wallMask *PixelMask) bool {
	if wallMask == nil || !wallMask.GetXY(x, y) {
		return false
	}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if !wallMask.InBounds(nx, ny) || !wallMask.GetXY(nx, ny) {
				return true
			}
		}
	}
	return false
}

func touchesWater(x, y int, waterMask *PixelMask) bool {
	if waterMask == nil {
		return false
	}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx, ny := x+dx, y+dy
			if waterMask.GetXY(nx, ny) {
				return true
			}
		}
	}
	return false
}

func averagePoint(points []image.Point) image.Point {
	if len(points) == 0 {
		return image.Point{}
	}
	var sx, sy int
	for _, p := range points {
		sx += p.X
		sy += p.Y
	}
	return image.Point{X: sx / len(points), Y: sy / len(points)}
}

func drawTurret(img *image.RGBA, mask *PixelMask, center image.Point, radius int, shape string, col color.RGBA) {
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if shape == "circular" && dx*dx+dy*dy > radius*radius {
				continue
			}
			x, y := center.X+dx, center.Y+dy
			if !mask.InBounds(x, y) {
				continue
			}
			mask.SetXY(x, y)
			img.Set(x, y, col)
		}
	}
}

func nearbyTurretExists(mask *PixelMask, center image.Point, radius int) bool {
	if mask == nil {
		return false
	}
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			if mask.GetXY(center.X+dx, center.Y+dy) {
				return true
			}
		}
	}
	return false
}

func snapPointToWall(center image.Point, wallMask *PixelMask, maxRadius int) (image.Point, bool) {
	if wallMask == nil {
		return image.Point{}, false
	}
	if wallMask.GetXY(center.X, center.Y) {
		return center, true
	}
	if maxRadius < 1 {
		maxRadius = 1
	}
	best := image.Point{}
	bestD2 := math.MaxInt
	found := false
	for r := 1; r <= maxRadius; r++ {
		minX := center.X - r
		maxX := center.X + r
		minY := center.Y - r
		maxY := center.Y + r
		for y := minY; y <= maxY; y++ {
			for x := minX; x <= maxX; x++ {
				if x != minX && x != maxX && y != minY && y != maxY {
					continue
				}
				if !wallMask.GetXY(x, y) {
					continue
				}
				dx := x - center.X
				dy := y - center.Y
				d2 := dx*dx + dy*dy
				if d2 < bestD2 {
					bestD2 = d2
					best = image.Point{X: x, Y: y}
					found = true
				}
			}
		}
		if found {
			return best, true
		}
	}
	return image.Point{}, false
}

func roadGateCentersForRoad(r *Road, layout *FortificationLayout) []image.Point {
	out := make([]image.Point, 0, 2)
	if r == nil || layout == nil || layout.Mask == nil || len(r.Points) < 2 {
		return out
	}
	prevID := 0
	if layout.Mask.InBounds(r.Points[0].Point.X, r.Points[0].Point.Y) {
		prevID = layout.WallIDByPixel[r.Points[0].Point.Y*layout.Mask.Width+r.Points[0].Point.X]
	}
	for i := 1; i < len(r.Points); i++ {
		p := r.Points[i].Point
		currID := 0
		if layout.Mask.InBounds(p.X, p.Y) {
			currID = layout.WallIDByPixel[p.Y*layout.Mask.Width+p.X]
		}
		if (prevID == 0 && currID > 0) || (prevID > 0 && currID == 0) {
			out = append(out, p)
		}
		prevID = currID
	}
	return out
}

func fortEstimateWallTangent(mid image.Point, wallMask *PixelMask) (float64, float64, bool) {
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
