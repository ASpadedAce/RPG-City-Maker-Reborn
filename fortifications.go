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
