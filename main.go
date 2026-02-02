package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"strconv"
	"time"

	"image/png"
	"os"
	"path/filepath"

	"image/jpeg"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/chai2010/webp"
)

func main() {
	a := app.NewWithID("com.example.rpgcitymaker")
	a.Settings().SetTheme(&CustomTheme{a.Settings().Theme()})
	w := a.NewWindow("RPG City Maker")
	w.Resize(fyne.NewSize(800, 600))

	settings, err := LoadSettings()
	if err != nil {
		log.Println("Error loading settings:", err)
		// Use default settings if loading fails
		settings = &Settings{Detail: 1, Roughness: 0, Width: 300, Height: 300, Lakes: 0, LakeSizeLower: 1, LakeSizeUpper: 5}
	}

	canvasImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}

	heightmapImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal("Failed to get user config dir:", err)
	}
	appConfigDir := filepath.Join(configDir, "rpgcitymakerreborn")

	canvasPath := filepath.Join(appConfigDir, "canvas.png")
	heightmapPath := filepath.Join(appConfigDir, "heightmap.png")

	w.SetOnClosed(func() {
		if err := settings.Save(); err != nil {
			log.Println("Error saving settings:", err)
		}

		if canvasImg.Image != nil {
			canvasFile, err := os.Create(canvasPath)
			if err != nil {
				log.Println("Failed to create canvas file:", err)
			} else {
				defer canvasFile.Close()
				if err := png.Encode(canvasFile, canvasImg.Image); err != nil {
					log.Println("Failed to encode canvas image:", err)
				}
			}
		}

		if heightmapImg.Image != nil {
			heightmapFile, err := os.Create(heightmapPath)
			if err != nil {
				log.Println("Failed to create heightmap file:", err)
			} else {
				defer heightmapFile.Close()
				if err := png.Encode(heightmapFile, heightmapImg.Image); err != nil {
					log.Println("Failed to encode heightmap image:", err)
				}
			}
		}
	})

	canvasFile, err := os.Open(canvasPath)
	if err == nil {
		defer canvasFile.Close()
		img, err := png.Decode(canvasFile)
		if err == nil {
			canvasImg.Image = img
		}
	}

	heightmapFile, err := os.Open(heightmapPath)
	if err == nil {
		defer heightmapFile.Close()
		img, err := png.Decode(heightmapFile)
		if err == nil {
			heightmapImg.Image = img
		}
	}

	if canvasImg.Image == nil || heightmapImg.Image == nil {
		// Initial image generation
		noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail), 100.0, settings.Seed)
		lakeImage, lakes := GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSizeLower, settings.LakeSizeUpper, noiseImg, settings.Seed)
		riverImage, riverPixels := GenerateRivers(settings.Width, settings.Height, settings.Rivers, settings.MinRiverWidth, settings.MaxRiverWidth, settings.RiverCurvyness, lakeImage, lakes, settings.Seed, noiseImg)
		finalImage := riverImage.(*image.RGBA)

		var flatLakePixels []image.Point
		for _, lake := range lakes {
			flatLakePixels = append(flatLakePixels, lake...)
		}
		allWaterPixels := append(flatLakePixels, riverPixels...)

		GenerateTrees(finalImage, allWaterPixels, settings.MinTreeSize, settings.MaxTreeSize, settings.TreeCoverage, settings.TreeClumpiness, settings.Seed)
		darkenedHeightmap := DarkenLakeAreas(noiseImg, allWaterPixels)
		compositeImg := ApplyRoughness(darkenedHeightmap, settings.Roughness)
		heightmapImg.Image = compositeImg
		canvasImg.Image = finalImage
	}

	detailLabel := widget.NewLabel(fmt.Sprintf("Detail: %.0f", settings.Detail))
	detailSlider := widget.NewSlider(1, 16)
	detailSlider.OnChanged = func(val float64) {
		settings.Detail = val
		detailLabel.SetText(fmt.Sprintf("Detail: %.0f", settings.Detail))
	}
	detailSlider.SetValue(settings.Detail)

	roughnessLabel := widget.NewLabel(fmt.Sprintf("Roughness: %.0f%%", settings.Roughness))
	roughnessSlider := widget.NewSlider(0, 100)
	roughnessSlider.OnChanged = func(val float64) {
		settings.Roughness = val
		roughnessLabel.SetText(fmt.Sprintf("Roughness: %.0f%%", settings.Roughness))
	}
	roughnessSlider.SetValue(settings.Roughness)

	lakesLabel := widget.NewLabel(fmt.Sprintf("Lakes: %d", settings.Lakes))
	lakesSlider := widget.NewSlider(0, 15)
	lakesSlider.OnChanged = func(val float64) {
		settings.Lakes = int(val)
		lakesLabel.SetText(fmt.Sprintf("Lakes: %d", settings.Lakes))
	}
	lakesSlider.SetValue(float64(settings.Lakes))

	lakeSizeLowerLabel := widget.NewLabel(fmt.Sprintf("Min Lake Size: %.0f%%", settings.LakeSizeLower))
	lakeSizeLowerSlider := widget.NewSlider(1, 100)
	lakeSizeUpperLabel := widget.NewLabel(fmt.Sprintf("Max Lake Size: %.0f%%", settings.LakeSizeUpper))
	lakeSizeUpperSlider := widget.NewSlider(1, 100)

	lakeSizeLowerSlider.OnChanged = func(val float64) {
		settings.LakeSizeLower = val
		if settings.LakeSizeLower > settings.LakeSizeUpper {
			settings.LakeSizeUpper = settings.LakeSizeLower
			lakeSizeUpperSlider.SetValue(settings.LakeSizeUpper)
		}
		lakeSizeLowerLabel.SetText(fmt.Sprintf("Min Lake Size: %.0f%%", settings.LakeSizeLower))
	}
	lakeSizeLowerSlider.SetValue(settings.LakeSizeLower)

	lakeSizeUpperSlider.OnChanged = func(val float64) {
		settings.LakeSizeUpper = val
		if settings.LakeSizeUpper < settings.LakeSizeLower {
			settings.LakeSizeLower = settings.LakeSizeUpper
			lakeSizeLowerSlider.SetValue(settings.LakeSizeLower)
		}
		lakeSizeUpperLabel.SetText(fmt.Sprintf("Max Lake Size: %.0f%%", settings.LakeSizeUpper))
	}
	lakeSizeUpperSlider.SetValue(settings.LakeSizeUpper)

	riversLabel := widget.NewLabel(fmt.Sprintf("Rivers: %d", settings.Rivers))
	riversSlider := widget.NewSlider(0, 5)
	riversSlider.OnChanged = func(val float64) {
		settings.Rivers = int(val)
		riversLabel.SetText(fmt.Sprintf("Rivers: %d", settings.Rivers))
	}
	riversSlider.SetValue(float64(settings.Rivers))

	minRiverWidthLabel := widget.NewLabel(fmt.Sprintf("Min River Width: %.0f%%", settings.MinRiverWidth))
	minRiverWidthSlider := widget.NewSlider(1, 100)
	maxRiverWidthLabel := widget.NewLabel(fmt.Sprintf("Max River Width: %.0f%%", settings.MaxRiverWidth))
	maxRiverWidthSlider := widget.NewSlider(1, 100)

	minRiverWidthSlider.OnChanged = func(val float64) {
		settings.MinRiverWidth = val
		if settings.MinRiverWidth > settings.MaxRiverWidth {
			settings.MaxRiverWidth = settings.MinRiverWidth
			maxRiverWidthSlider.SetValue(settings.MaxRiverWidth)
		}
		minRiverWidthLabel.SetText(fmt.Sprintf("Min River Width: %.0f%%", settings.MinRiverWidth))
	}
	minRiverWidthSlider.SetValue(settings.MinRiverWidth)

	maxRiverWidthSlider.OnChanged = func(val float64) {
		settings.MaxRiverWidth = val
		if settings.MaxRiverWidth < settings.MinRiverWidth {
			settings.MinRiverWidth = settings.MaxRiverWidth
			minRiverWidthSlider.SetValue(settings.MinRiverWidth)
		}
		maxRiverWidthLabel.SetText(fmt.Sprintf("Max River Width: %.0f%%", settings.MaxRiverWidth))
	}
	maxRiverWidthSlider.SetValue(settings.MaxRiverWidth)

	riverCurvynessLabel := widget.NewLabel(fmt.Sprintf("River Curvyness: %.0f%%", settings.RiverCurvyness))
	riverCurvynessSlider := widget.NewSlider(0, 100)
	riverCurvynessSlider.OnChanged = func(val float64) {
		settings.RiverCurvyness = val
		riverCurvynessLabel.SetText(fmt.Sprintf("River Curvyness: %.0f%%", settings.RiverCurvyness))
	}
	riverCurvynessSlider.SetValue(settings.RiverCurvyness)

	minTreeSizeLabel := widget.NewLabel(fmt.Sprintf("Min Tree Size: %.0fpx", settings.MinTreeSize))
	minTreeSizeSlider := widget.NewSlider(1, 150)
	maxTreeSizeLabel := widget.NewLabel(fmt.Sprintf("Max Tree Size: %.0fpx", settings.MaxTreeSize))
	maxTreeSizeSlider := widget.NewSlider(1, 150)

	minTreeSizeSlider.OnChanged = func(val float64) {
		settings.MinTreeSize = val
		if settings.MinTreeSize > settings.MaxTreeSize {
			settings.MaxTreeSize = settings.MinTreeSize
			maxTreeSizeSlider.SetValue(settings.MaxTreeSize)
		}
		minTreeSizeLabel.SetText(fmt.Sprintf("Min Tree Size: %.0fpx", settings.MinTreeSize))
	}
	minTreeSizeSlider.SetValue(settings.MinTreeSize)

	maxTreeSizeSlider.OnChanged = func(val float64) {
		settings.MaxTreeSize = val
		if settings.MaxTreeSize < settings.MinTreeSize {
			settings.MinTreeSize = settings.MaxTreeSize
			minTreeSizeSlider.SetValue(settings.MinTreeSize)
		}
		maxTreeSizeLabel.SetText(fmt.Sprintf("Max Tree Size: %.0fpx", settings.MaxTreeSize))
	}
	maxTreeSizeSlider.SetValue(settings.MaxTreeSize)

	treeCoverageLabel := widget.NewLabel(fmt.Sprintf("Tree Coverage: %.0f%%", settings.TreeCoverage))
	treeCoverageSlider := widget.NewSlider(1, 100)
	treeCoverageSlider.OnChanged = func(val float64) {
		settings.TreeCoverage = val
		treeCoverageLabel.SetText(fmt.Sprintf("Tree Coverage: %.0f%%", settings.TreeCoverage))
	}
	treeCoverageSlider.SetValue(settings.TreeCoverage)

	treeClumpinessLabel := widget.NewLabel(fmt.Sprintf("Tree Clumpiness: %.0f%%", settings.TreeClumpiness))
	treeClumpinessSlider := widget.NewSlider(0, 100)
	treeClumpinessSlider.OnChanged = func(val float64) {
		settings.TreeClumpiness = val
		treeClumpinessLabel.SetText(fmt.Sprintf("Tree Clumpiness: %.0f%%", settings.TreeClumpiness))
	}
	treeClumpinessSlider.SetValue(settings.TreeClumpiness)

	errorLabel := canvas.NewText("", color.RGBA{R: 255, A: 255})
	errorLabel.TextSize = 12
	errorLabel.Hide()

	var generateBtn *widget.Button
	progressBar := NewTextOverlayProgressBar()
	exportCanvasBtn := widget.NewButton("Export Canvas", func() {
		showSaveDialog(w, canvasImg.Image, settings)
	})
	exportHeightmapBtn := widget.NewButton("Export Heightmap", func() {
		showSaveDialog(w, heightmapImg.Image, settings)
	})

	generateBtn = widget.NewButton("Generate", func() {
		go func() {
			fyne.Do(func() {
				generateBtn.Disable()
			})
			defer func() {
				fyne.Do(func() {
					generateBtn.Enable()
				})
			}()

			steps := 7
			currentStep := 0

			// Step 1: Generating Heightmap
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Generating Heightmap", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail), 100.0, settings.Seed)

			// Step 2: Generating Lakes
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Generating Lakes", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			lakeImage, lakes := GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSizeLower, settings.LakeSizeUpper, noiseImg, settings.Seed)

			// Step 3: Generating Rivers
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Generating Rivers", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			riverImage, riverPixels := GenerateRivers(settings.Width, settings.Height, settings.Rivers, settings.MinRiverWidth, settings.MaxRiverWidth, settings.RiverCurvyness, lakeImage, lakes, settings.Seed, noiseImg)

			finalImage := riverImage.(*image.RGBA)
			var flatLakePixels []image.Point
			for _, lake := range lakes {
				flatLakePixels = append(flatLakePixels, lake...)
			}
			allWaterPixels := append(flatLakePixels, riverPixels...)

			// Step 4: Darkening Water Areas
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Darkening Water Areas", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			darkenedHeightmap := DarkenLakeAreas(noiseImg, allWaterPixels)

			// Step 5: Applying Roughness
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Applying Roughness", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			compositeImg := ApplyRoughness(darkenedHeightmap, settings.Roughness)

			// Step 6: Generating Trees
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Generating Trees", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
			})
			GenerateTrees(finalImage, allWaterPixels, settings.MinTreeSize, settings.MaxTreeSize, settings.TreeCoverage, settings.TreeClumpiness, settings.Seed)

			// Step 7: Finalizing Images
			currentStep++
			fyne.Do(func() {
				progressBar.SetText(fmt.Sprintf("Step %d/%d: Finalizing Images", currentStep, steps))
				progressBar.SetValue(float64(currentStep) / float64(steps))
				heightmapImg.Image = compositeImg
				heightmapImg.Refresh()
				canvasImg.Image = finalImage
				canvasImg.Refresh()

				progressBar.SetText("Generation Complete!")
			})
		}()
	})

	widthEntry := widget.NewEntry()
	widthEntry.SetText(strconv.Itoa(settings.Width))
	widthEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err != nil {
			errorLabel.Text = "Width must be a number"
			errorLabel.Show()
			generateBtn.Disable()
			return
		}
		errorLabel.Hide()
		generateBtn.Enable()
		settings.Width = val
	}
	heightEntry := widget.NewEntry()
	heightEntry.SetText(strconv.Itoa(settings.Height))
	heightEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err != nil {
			errorLabel.Text = "Height must be a number"
			errorLabel.Show()
			generateBtn.Disable()
			return
		}
		errorLabel.Hide()
		generateBtn.Enable()
		settings.Height = val
	}

	seedEntry := widget.NewEntry()
	seedEntry.SetText(strconv.FormatInt(settings.Seed, 10))
	seedEntry.OnChanged = func(s string) {
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			errorLabel.Text = "Seed must be a number"
			errorLabel.Show()
			generateBtn.Disable()
			return
		}
		errorLabel.Hide()
		generateBtn.Enable()
		settings.Seed = val
	}

	randomizeBtn := widget.NewButton("Randomize", func() {
		settings.Seed = time.Now().UnixNano()
		seedEntry.SetText(strconv.FormatInt(settings.Seed, 10))
	})

	terrainTab := container.NewTabItem("Terrain", container.NewVBox(
		detailLabel,
		detailSlider,
		roughnessLabel,
		roughnessSlider,

		widget.NewLabel(""), // Spacer

		minTreeSizeLabel,
		minTreeSizeSlider,
		maxTreeSizeLabel,
		maxTreeSizeSlider,
		treeCoverageLabel,
		treeCoverageSlider,
		treeClumpinessLabel,
		treeClumpinessSlider,
	))

	waterTab := container.NewTabItem("Water", container.NewVBox(
		lakesLabel,
		lakesSlider,
		lakeSizeLowerLabel,
		lakeSizeLowerSlider,
		lakeSizeUpperLabel,
		lakeSizeUpperSlider,

		widget.NewLabel(""), // Spacer

		riversLabel,
		riversSlider,
		minRiverWidthLabel,
		minRiverWidthSlider,
		maxRiverWidthLabel,
		maxRiverWidthSlider,
		riverCurvynessLabel,
		riverCurvynessSlider,
	))

	imageTab := container.NewTabItem("Image", container.NewVBox(
		widget.NewLabel("Width:"),
		widthEntry,
		widget.NewLabel("Height:"),
		heightEntry,
		widget.NewLabel("Seed:"),
		seedEntry,
		randomizeBtn,
		generateBtn,
		errorLabel,
		progressBar,
		widget.NewSeparator(),
		exportCanvasBtn,
		exportHeightmapBtn,
	))

	tabs := container.NewAppTabs(
		imageTab,
		terrainTab,
		waterTab,
	)

	left := container.NewVBox(
		widget.NewLabel("RPG City Maker Reborn"),
		tabs,
	)

	right := container.NewGridWithRows(2,
		canvasImg,
		heightmapImg,
	)

	split := container.NewHSplit(
		left,
		right,
	)
	split.SetOffset(0.3)

	w.SetContent(split)
	w.ShowAndRun()
}

func showSaveDialog(win fyne.Window, img image.Image, settings *Settings) {
	fileNameEntry := widget.NewEntry()
	fileNameEntry.SetPlaceHolder("image")

	formatSelect := widget.NewSelect([]string{"PNG", "JPG", "WEBP"}, nil)
	formatSelect.SetSelected("PNG")

	pathLabel := widget.NewLabel(settings.LastExportPath)

	browseBtn := widget.NewButton("Browse", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				log.Println("Error opening folder dialog:", err)
				return
			}
			if uri == nil {
				return
			}
			settings.LastExportPath = uri.Path()
			pathLabel.SetText(settings.LastExportPath)
		}, win)
	})

	content := container.NewVBox(
		widget.NewLabel("Save to:"),
		container.NewBorder(nil, nil, nil, browseBtn, pathLabel),
		widget.NewLabel("Filename:"),
		fileNameEntry,
		widget.NewLabel("Format:"),
		formatSelect,
	)

	saveDialog := dialog.NewCustom("Export Image", "Cancel", content, win)

	saveBtn := widget.NewButton("Save", func() {
		fileName := fileNameEntry.Text
		if fileName == "" {
			return
		}

		filePath := filepath.Join(pathLabel.Text, fileName+"."+strings.ToLower(formatSelect.Selected))
		settings.LastExportPath = filepath.Dir(filePath)

		file, err := os.Create(filePath)
		if err != nil {
			log.Println("Error creating file:", err)
			return
		}
		defer file.Close()

		switch formatSelect.Selected {
		case "PNG":
			err = png.Encode(file, img)
		case "JPG":
			err = jpeg.Encode(file, img, nil)
		case "WEBP":
			err = webp.Encode(file, img, &webp.Options{Lossless: true})
		}
		if err != nil {
			log.Println("Failed to encode image:", err)
		}
		saveDialog.Hide()
	})
	saveBtn.Disable()

	fileNameEntry.OnChanged = func(text string) {
		if text == "" {
			saveBtn.Disable()
		} else {
			saveBtn.Enable()
		}
	}

	saveDialog.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Cancel", func() {
			saveDialog.Hide()
		}),
		saveBtn,
	})

	saveDialog.Show()
}
