package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/chai2010/webp"
)

func main() {
	// Initialize the Fyne application and window
	a := app.NewWithID("com.example.rpgcitymaker")
	w := a.NewWindow("RPG City Maker")
	w.Resize(fyne.NewSize(800, 600))

	// Load settings from file, or use defaults if loading fails
	settings, err := LoadSettings()
	if err != nil {
		log.Println("Error loading settings:", err)
		// Use default settings if loading fails
		settings = &Settings{Detail: 1, Roughness: 0, Width: 300, Height: 300, Lakes: 0, LakeSizeLower: 1, LakeSizeUpper: 5}
	}

	// Create canvas objects for displaying the generated images
	canvasImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}
	heightmapImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}
	bumpmapImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}

	// Initialize slices to store generated map features
	var lakes [][]image.Point
	var buildings [][]image.Point
	var waterMask *PixelMask
	var riverMask *PixelMask
	var treeMask *PixelMask
	var buildingMask *PixelMask
	var roadMask *PixelMask
	var bridgeMask *PixelMask
	var exitRoadMask *PixelMask

	// Set up application configuration directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal("Failed to get user config dir:", err)
	}
	appConfigDir := filepath.Join(configDir, "rpgcitymakerreborn")
	canvasPath := filepath.Join(appConfigDir, "canvas.png")
	heightmapPath := filepath.Join(appConfigDir, "heightmap.png")
	bumpmapPath := filepath.Join(appConfigDir, "bumpmap.png")

	// Save settings and images on window close
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

		if bumpmapImg.Image != nil {
			bumpmapFile, err := os.Create(bumpmapPath)
			if err != nil {
				log.Println("Failed to create bumpmap file:", err)
			} else {
				defer bumpmapFile.Close()
				if err := png.Encode(bumpmapFile, bumpmapImg.Image); err != nil {
					log.Println("Failed to encode bumpmap image:", err)
				}
			}
		}
	})

	// Load previously saved images
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

	bumpmapFile, err := os.Open(bumpmapPath)
	if err == nil {
		defer bumpmapFile.Close()
		img, err := png.Decode(bumpmapFile)
		if err == nil {
			bumpmapImg.Image = img
		}
	}

	// Generate initial images if none are loaded
	if canvasImg.Image == nil || heightmapImg.Image == nil {

		// Step 1: Generating Heightmap
		seedProvider := NewSeedProvider(settings.Seed)
		noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail), 100.0, seedProvider.Next())

		// Step 2: Generating Lakes
		var lakeImage image.Image

		lakeImage, lakes = GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSizeLower, settings.LakeSizeUpper, seedProvider.Next(), settings.LakeEdgeRoughness, settings.LakeShape)

		// Step 3: Generating Rivers
		var riverImage image.Image
		riverImage, riverMask = GenerateRivers(settings.Width, settings.Height, settings.Rivers, settings.MinRiverWidth, settings.MaxRiverWidth, settings.RiverCurvyness, lakeImage, lakes, seedProvider.Next(), noiseImg, settings.RiverWidthVariability, settings.RiverEdgeRoughness)

		finalImage := riverImage.(*image.RGBA)
		waterMask = BuildMaskFromLakes(settings.Width, settings.Height, lakes)
		if riverMask != nil {
			waterMask.Merge(riverMask)
		}
		// Step 4: Generating Roads
		var roadAnchors []image.Point
		roadMask, bridgeMask, exitRoadMask, roadAnchors = GenerateRoads(finalImage, settings.Width, settings.Height, settings, waterMask, seedProvider.Next())

		// Step 5: Generating Buildings
		buildings, buildingMask = GenerateBuildings(finalImage, settings.Width, settings.Height, settings, roadAnchors, waterMask, roadMask, exitRoadMask, seedProvider.Next())

		// Step 6: Generating Trees
		treeMask = GenerateTrees(finalImage, waterMask, roadMask, buildingMask, settings.MinTreeSize, settings.MaxTreeSize, settings.TreeCoverage, settings.TreeClumpiness, seedProvider.Next())

		// Step 7: Darkening Water Areas
		darkenedHeightmap := DarkenLakeAreas(noiseImg, waterMask)

		// Step 8: Flattening Building Areas
		flattenedBuildingHeightmap := FlattenBuildingAreas(darkenedHeightmap.(*image.RGBA), buildings, settings.Width, settings.Height)

		// Step 9: Flattening Road Areas
		flattenedHeightmap := FlattenRoadAreas(flattenedBuildingHeightmap, roadMask)

		// Step 10: Applying Roughness
		compositeImg := ApplyRoughness(flattenedHeightmap, settings.Roughness)

		// Step 11: Generating Bump Map
		bumpMap := GenerateBumpMap(compositeImg.(*image.RGBA), settings.Width, settings.Height, 0.10)

		heightmapImg.Image = compositeImg
		bumpmapImg.Image = bumpMap

		canvasImg.Image = finalImage

	}
	var generateBtn *widget.Button
	errorStates := make(map[string]bool)

	// ... (other code)

	// Main generation button and logic
	generateBtn = widget.NewButton("Generate", func() {
		// ... (generation logic)
	})

	// Function to update the generate button state
	updateGenerateBtnState := func() {
		for _, hasError := range errorStates {
			if hasError {
				generateBtn.Disable()
				return
			}
		}
		generateBtn.Enable()
	}

	detailSlider := newNumericInputSlider(1, 16, settings.Detail, "%.0f", "Detail")
	detailSlider.entry.OnChanged = func(s string) {
		detailSlider.validate(s, func(hasError bool) {
			errorStates["detail"] = hasError
			updateGenerateBtnState()
		})
	}
	detailSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := detailSlider.value.Get()
		settings.Detail = val
	}))

	roughnessSlider := newNumericInputSlider(0, 100, settings.Roughness, "%.0f%%", "Roughness")
	roughnessSlider.entry.OnChanged = func(s string) {
		roughnessSlider.validate(s, func(hasError bool) {
			errorStates["roughness"] = hasError
			updateGenerateBtnState()
		})
	}
	roughnessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := roughnessSlider.value.Get()
		settings.Roughness = val
	}))
	// Create UI elements for controlling lake generation settings
	lakesSlider := newNumericInputSlider(0, 15, float64(settings.Lakes), "%.0f", "Lakes")
	lakesSlider.entry.OnChanged = func(s string) {
		lakesSlider.validate(s, func(hasError bool) {
			errorStates["lakes"] = hasError
			updateGenerateBtnState()
		})
	}
	lakesSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := lakesSlider.value.Get()
		settings.Lakes = int(val)
	}))

	lakeSizeLowerSlider := newNumericInputSlider(1, 100, settings.LakeSizeLower, "%.0f%%", "Min Lake Size")
	lakeSizeLowerSlider.entry.OnChanged = func(s string) {
		lakeSizeLowerSlider.validate(s, func(hasError bool) {
			errorStates["lakeSizeLower"] = hasError
			updateGenerateBtnState()
		})
	}
	lakeSizeLowerSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := lakeSizeLowerSlider.value.Get()
		settings.LakeSizeLower = val
	}))

	lakeSizeUpperSlider := newNumericInputSlider(1, 100, settings.LakeSizeUpper, "%.0f%%", "Max Lake Size")
	lakeSizeUpperSlider.entry.OnChanged = func(s string) {
		lakeSizeUpperSlider.validate(s, func(hasError bool) {
			errorStates["lakeSizeUpper"] = hasError
			updateGenerateBtnState()
		})
	}
	lakeSizeUpperSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := lakeSizeUpperSlider.value.Get()
		settings.LakeSizeUpper = val
	}))

	lakeEdgeRoughnessSlider := newNumericInputSlider(0, 100, settings.LakeEdgeRoughness, "%.0f%%", "Lake Edge Roughness")
	lakeEdgeRoughnessSlider.entry.OnChanged = func(s string) {
		lakeEdgeRoughnessSlider.validate(s, func(hasError bool) {
			errorStates["lakeEdgeRoughness"] = hasError
			updateGenerateBtnState()
		})
	}
	lakeEdgeRoughnessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := lakeEdgeRoughnessSlider.value.Get()
		settings.LakeEdgeRoughness = val
	}))

	lakeShapeLabel := widget.NewLabel("Lake Shape:")
	lakeShapeSelect := widget.NewSelect([]string{"circle", "oval", "procedural"}, func(s string) {
		settings.LakeShape = s
	})
	lakeShapeSelect.SetSelected(settings.LakeShape)

	riversSlider := newNumericInputSlider(0, 5, float64(settings.Rivers), "%.0f", "Rivers")
	riversSlider.entry.OnChanged = func(s string) {
		riversSlider.validate(s, func(hasError bool) {
			errorStates["rivers"] = hasError
			updateGenerateBtnState()
		})
	}
	riversSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := riversSlider.value.Get()
		settings.Rivers = int(val)
	}))

	minRiverWidthSlider := newNumericInputSlider(1, 100, settings.MinRiverWidth, "%.0f%%", "Min River Width")
	minRiverWidthSlider.entry.OnChanged = func(s string) {
		minRiverWidthSlider.validate(s, func(hasError bool) {
			errorStates["minRiverWidth"] = hasError
			updateGenerateBtnState()
		})
	}
	minRiverWidthSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minRiverWidthSlider.value.Get()
		settings.MinRiverWidth = val
	}))

	maxRiverWidthSlider := newNumericInputSlider(1, 100, settings.MaxRiverWidth, "%.0f%%", "Max River Width")
	maxRiverWidthSlider.entry.OnChanged = func(s string) {
		maxRiverWidthSlider.validate(s, func(hasError bool) {
			errorStates["maxRiverWidth"] = hasError
			updateGenerateBtnState()
		})
	}
	maxRiverWidthSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := maxRiverWidthSlider.value.Get()
		settings.MaxRiverWidth = val
	}))

	riverCurvynessSlider := newNumericInputSlider(0, 100, settings.RiverCurvyness, "%.0f%%", "River Curvyness")
	riverCurvynessSlider.entry.OnChanged = func(s string) {
		riverCurvynessSlider.validate(s, func(hasError bool) {
			errorStates["riverCurvyness"] = hasError
			updateGenerateBtnState()
		})
	}
	riverCurvynessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := riverCurvynessSlider.value.Get()
		settings.RiverCurvyness = val
	}))

	riverWidthVariabilitySlider := newNumericInputSlider(0, 100, settings.RiverWidthVariability, "%.0f%%", "River Width Variability")
	riverWidthVariabilitySlider.entry.OnChanged = func(s string) {
		riverWidthVariabilitySlider.validate(s, func(hasError bool) {
			errorStates["riverWidthVariability"] = hasError
			updateGenerateBtnState()
		})
	}
	riverWidthVariabilitySlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := riverWidthVariabilitySlider.value.Get()
		settings.RiverWidthVariability = val
	}))

	riverEdgeRoughnessSlider := newNumericInputSlider(0, 100, settings.RiverEdgeRoughness, "%.0f%%", "River Edge Roughness")
	riverEdgeRoughnessSlider.entry.OnChanged = func(s string) {
		riverEdgeRoughnessSlider.validate(s, func(hasError bool) {
			errorStates["riverEdgeRoughness"] = hasError
			updateGenerateBtnState()
		})
	}
	riverEdgeRoughnessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := riverEdgeRoughnessSlider.value.Get()
		settings.RiverEdgeRoughness = val
	}))

	minTreeSizeSlider := newNumericInputSlider(1, 150, settings.MinTreeSize, "%.0fpx", "Min Tree Size")
	minTreeSizeSlider.entry.OnChanged = func(s string) {
		minTreeSizeSlider.validate(s, func(hasError bool) {
			errorStates["minTreeSize"] = hasError
			updateGenerateBtnState()
		})
	}
	minTreeSizeSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minTreeSizeSlider.value.Get()
		settings.MinTreeSize = val
	}))

	maxTreeSizeSlider := newNumericInputSlider(1, 150, settings.MaxTreeSize, "%.0fpx", "Max Tree Size")
	maxTreeSizeSlider.entry.OnChanged = func(s string) {
		maxTreeSizeSlider.validate(s, func(hasError bool) {
			errorStates["maxTreeSize"] = hasError
			updateGenerateBtnState()
		})
	}
	maxTreeSizeSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := maxTreeSizeSlider.value.Get()
		settings.MaxTreeSize = val
	}))

	treeCoverageSlider := newNumericInputSlider(1, 100, settings.TreeCoverage, "%.0f%%", "Tree Coverage")
	treeCoverageSlider.entry.OnChanged = func(s string) {
		treeCoverageSlider.validate(s, func(hasError bool) {
			errorStates["treeCoverage"] = hasError
			updateGenerateBtnState()
		})
	}
	treeCoverageSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := treeCoverageSlider.value.Get()
		settings.TreeCoverage = val
	}))

	treeClumpinessSlider := newNumericInputSlider(0, 100, settings.TreeClumpiness, "%.0f%%", "Tree Clumpiness")
	treeClumpinessSlider.entry.OnChanged = func(s string) {
		treeClumpinessSlider.validate(s, func(hasError bool) {
			errorStates["treeClumpiness"] = hasError
			updateGenerateBtnState()
		})
	}
	treeClumpinessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := treeClumpinessSlider.value.Get()
		settings.TreeClumpiness = val
	}))
	minRoadWidthSlider := newNumericInputSliderWithStep(minRoadWidthPercent, maxRoadWidthPercent, settings.MinRoadWidth, roadWidthPercentStep, "%.1f%%", "Min Road Width")
	minRoadWidthSlider.entry.OnChanged = func(s string) {
		minRoadWidthSlider.validate(s, func(hasError bool) {
			errorStates["minRoadWidth"] = hasError
			updateGenerateBtnState()
		})
	}
	minRoadWidthSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minRoadWidthSlider.value.Get()
		settings.MinRoadWidth = val
	}))

	maxRoadWidthSlider := newNumericInputSliderWithStep(minRoadWidthPercent, maxRoadWidthPercent, settings.MaxRoadWidth, roadWidthPercentStep, "%.1f%%", "Max Road Width")
	maxRoadWidthSlider.entry.OnChanged = func(s string) {
		maxRoadWidthSlider.validate(s, func(hasError bool) {
			errorStates["maxRoadWidth"] = hasError
			updateGenerateBtnState()
		})
	}
	maxRoadWidthSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := maxRoadWidthSlider.value.Get()
		settings.MaxRoadWidth = val
	}))

	roadExitsSlider := newNumericInputSlider(0, 100, float64(settings.RoadExits), "%.0f", "Road Exits")
	roadExitsSlider.entry.OnChanged = func(s string) {
		roadExitsSlider.validate(s, func(hasError bool) {
			errorStates["roadExits"] = hasError
			updateGenerateBtnState()
		})
	}
	roadExitsSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := roadExitsSlider.value.Get()
		settings.RoadExits = int(val)
	}))

	roadCurvynessSlider := newNumericInputSlider(0, 100, settings.RoadCurvyness, "%.0f%%", "Road Curvyness")
	roadCurvynessSlider.entry.OnChanged = func(s string) {
		roadCurvynessSlider.validate(s, func(hasError bool) {
			errorStates["roadCurvyness"] = hasError
			updateGenerateBtnState()
		})
	}
	roadCurvynessSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := roadCurvynessSlider.value.Get()
		settings.RoadCurvyness = val
	}))

	roadDistributionSlider := newNumericInputSlider(0, 100, settings.RoadDistribution, "%.0f%%", "Distribution")
	roadDistributionSlider.entry.OnChanged = func(s string) {
		roadDistributionSlider.validate(s, func(hasError bool) {
			errorStates["roadDistribution"] = hasError
			updateGenerateBtnState()
		})
	}
	roadDistributionSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := roadDistributionSlider.value.Get()
		settings.RoadDistribution = val
	}))

	minRoadAngleSlider := newNumericInputSlider(0, 180, settings.MinRoadAngle, "%.0f°", "Minimum Road Angle")
	minRoadAngleSlider.entry.OnChanged = func(s string) {
		minRoadAngleSlider.validate(s, func(hasError bool) {
			errorStates["minRoadAngle"] = hasError
			updateGenerateBtnState()
		})
	}
	minRoadAngleSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minRoadAngleSlider.value.Get()
		settings.MinRoadAngle = val
	}))

	// Create UI elements for error display and action buttons
	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord
	errorLabel.Hide()
	progressLabel := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	progressContainer := container.NewVBox(progressLabel, progressBar)
	progressContainer.Hide()
	exportCanvasBtn := widget.NewButton("Export Canvas", func() {
		showSaveDialog(w, canvasImg.Image, settings)
	})
	exportHeightmapBtn := widget.NewButton("Export Heightmap", func() {
		showSaveDialog(w, heightmapImg.Image, settings)
	})
	exportBumpmapBtn := widget.NewButton("Export Bump Map", func() {
		showSaveDialog(w, bumpmapImg.Image, settings)
	})
	exportMasksBtn := widget.NewButton("Export Masks", func() {
		showMasksSaveDialog(w, canvasImg.Image, heightmapImg.Image, bumpmapImg.Image, settings, lakes, riverMask, treeMask, roadMask, bridgeMask, buildingMask)
	})
	// Main generation button and logic
	generateBtn = widget.NewButton("Generate", func() {
		go func() {
			// Disable button and show progress bar during generation
			fyne.Do(func() {
				generateBtn.Disable()
				progressContainer.Show()
			})
			defer func() {
				// Re-enable button and hide progress bar after generation
				fyne.Do(func() {
					generateBtn.Enable()
					progressContainer.Hide()
				})
			}()
			seedProvider := NewSeedProvider(settings.Seed)
			// Step 1: Generating Heightmap
			fyne.Do(func() {
				progressLabel.SetText("Step 1 of 11: Generating Heightmap")
				progressBar.SetValue(1.0 / 11.0)
			})
			noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail), 100.0, seedProvider.Next())

			// Step 2: Generating Lakes

			fyne.Do(func() {
				progressLabel.SetText("Step 2 of 11: Generating Lakes")
				progressBar.SetValue(2.0 / 11.0)
			})
			var lakeImage image.Image
			lakeImage, lakes = GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSizeLower, settings.LakeSizeUpper, seedProvider.Next(), settings.LakeEdgeRoughness, settings.LakeShape)

			// Step 3: Generating Rivers

			fyne.Do(func() {
				progressLabel.SetText("Step 3 of 11: Generating Rivers")
				progressBar.SetValue(3.0 / 11.0)
			})
			var riverImage image.Image
			riverImage, riverMask = GenerateRivers(settings.Width, settings.Height, settings.Rivers, settings.MinRiverWidth, settings.MaxRiverWidth, settings.RiverCurvyness, lakeImage, lakes, seedProvider.Next(), noiseImg, settings.RiverWidthVariability, settings.RiverEdgeRoughness)

			finalImage := riverImage.(*image.RGBA)
			waterMask = BuildMaskFromLakes(settings.Width, settings.Height, lakes)
			if riverMask != nil {
				waterMask.Merge(riverMask)
			}

			// Step 4: Generating Roads

			fyne.Do(func() {
				progressLabel.SetText("Step 4 of 11: Generating Roads")
				progressBar.SetValue(4.0 / 11.0)
			})
			var roadAnchors []image.Point
			roadMask, bridgeMask, exitRoadMask, roadAnchors = GenerateRoads(finalImage, settings.Width, settings.Height, settings, waterMask, seedProvider.Next())

			// Step 5: Generating Buildings

			fyne.Do(func() {
				progressLabel.SetText("Step 5 of 11: Generating Buildings")
				progressBar.SetValue(5.0 / 11.0)
			})
			buildings, buildingMask = GenerateBuildings(finalImage, settings.Width, settings.Height, settings, roadAnchors, waterMask, roadMask, exitRoadMask, seedProvider.Next())

			// Step 6: Darkening Water Areas

			fyne.Do(func() {
				progressLabel.SetText("Step 6 of 11: Darkening Water Areas")
				progressBar.SetValue(6.0 / 11.0)
			})
			darkenedHeightmap := DarkenLakeAreas(noiseImg, waterMask)

			// Step 7: Flattening Building Areas

			fyne.Do(func() {
				progressLabel.SetText("Step 7 of 11: Flattening Building Areas")
				progressBar.SetValue(7.0 / 11.0)
			})
			flattenedBuildingHeightmap := FlattenBuildingAreas(darkenedHeightmap.(*image.RGBA), buildings, settings.Width, settings.Height)

			fyne.Do(func() {
				progressLabel.SetText("Step 8 of 11: Flattening Road Areas")
				progressBar.SetValue(8.0 / 11.0)
			})
			flattenedHeightmap := FlattenRoadAreas(flattenedBuildingHeightmap, roadMask)

			// Step 8: Applying Roughness

			fyne.Do(func() {
				progressLabel.SetText("Step 9 of 11: Applying Roughness")
				progressBar.SetValue(9.0 / 11.0)
			})
			compositeImg := ApplyRoughness(flattenedHeightmap, settings.Roughness)

			// Step 9: Generating Trees

			fyne.Do(func() {
				progressLabel.SetText("Step 10 of 11: Generating Trees")
				progressBar.SetValue(10.0 / 11.0)
			})
			treeMask = GenerateTrees(finalImage, waterMask, roadMask, buildingMask, settings.MinTreeSize, settings.MaxTreeSize, settings.TreeCoverage, settings.TreeClumpiness, seedProvider.Next())

			// Step 10: Generating Bump Map

			fyne.Do(func() {
				progressLabel.SetText("Step 11 of 11: Generating Bump Map")
				progressBar.SetValue(1.0)
			})
			bumpMap := GenerateBumpMap(compositeImg.(*image.RGBA), settings.Width, settings.Height, 0.10)

			// Step 11: Finalizing Images

			fyne.Do(func() {
				heightmapImg.Image = compositeImg
				heightmapImg.Refresh()
				bumpmapImg.Image = bumpMap
				bumpmapImg.Refresh()
				canvasImg.Image = finalImage
				canvasImg.Refresh()
			})
		}()
	})
	// Create UI elements for image dimensions and seed
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
	// Create tabs for organizing settings
	terrainTab := container.NewTabItem("Terrain", container.NewVBox(
		detailSlider,
		roughnessSlider,

		widget.NewLabel(""), // Spacer

		minTreeSizeSlider,
		maxTreeSizeSlider,
		treeCoverageSlider,
		treeClumpinessSlider,
	))

	waterTab := container.NewTabItem("Water", container.NewVBox(
		lakesSlider,
		lakeSizeLowerSlider,
		lakeSizeUpperSlider,
		lakeEdgeRoughnessSlider,
		lakeShapeLabel,
		lakeShapeSelect,

		widget.NewLabel(""), // Spacer

		riversSlider,
		minRiverWidthSlider,
		maxRiverWidthSlider,
		riverCurvynessSlider,
		riverWidthVariabilitySlider,
		riverEdgeRoughnessSlider,
	))

	roadsTab := container.NewTabItem("Roads", container.NewVBox(
		minRoadWidthSlider,
		maxRoadWidthSlider,
		roadExitsSlider,
		minRoadAngleSlider,
		roadCurvynessSlider,
		roadDistributionSlider,
	))
	numBuildingsSlider := newNumericInputSlider(0, 10000, float64(settings.NumBuildings), "%.0f", "Number of Buildings")
	numBuildingsSlider.entry.OnChanged = func(s string) {
		numBuildingsSlider.validate(s, func(hasError bool) {
			errorStates["numBuildings"] = hasError
			updateGenerateBtnState()
		})
	}
	numBuildingsSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := numBuildingsSlider.value.Get()
		settings.NumBuildings = int(val)
	}))

	minBuildingSizeSlider := newNumericInputSliderWithStep(minBuildingSizePercent, maxBuildingSizePercent, settings.MinBuildingSize, buildingSizePercentStep, "%.1f%%", "Min Building Size")
	minBuildingSizeSlider.entry.OnChanged = func(s string) {
		minBuildingSizeSlider.validate(s, func(hasError bool) {
			errorStates["minBuildingSize"] = hasError
			updateGenerateBtnState()
		})
	}
	minBuildingSizeSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minBuildingSizeSlider.value.Get()
		settings.MinBuildingSize = val
	}))

	maxBuildingSizeSlider := newNumericInputSliderWithStep(minBuildingSizePercent, maxBuildingSizePercent, settings.MaxBuildingSize, buildingSizePercentStep, "%.1f%%", "Max Building Size")
	maxBuildingSizeSlider.entry.OnChanged = func(s string) {
		maxBuildingSizeSlider.validate(s, func(hasError bool) {
			errorStates["maxBuildingSize"] = hasError
			updateGenerateBtnState()
		})
	}
	maxBuildingSizeSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := maxBuildingSizeSlider.value.Get()
		settings.MaxBuildingSize = val
	}))

	buildingDistributionSlider := newNumericInputSlider(0, 100, settings.BuildingDistribution, "%.0f%%", "Building Distribution")
	buildingDistributionSlider.entry.OnChanged = func(s string) {
		buildingDistributionSlider.validate(s, func(hasError bool) {
			errorStates["buildingDistribution"] = hasError
			updateGenerateBtnState()
		})
	}
	buildingDistributionSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := buildingDistributionSlider.value.Get()
		settings.BuildingDistribution = val
	}))

	buildingShapeLabel := widget.NewLabel("Building Shape:")
	buildingShapeSelect := widget.NewSelect([]string{"squares", "circles", "rectangles", "mixed", "procedural"}, func(s string) {
		settings.BuildingShape = s
	})
	buildingShapeSelect.SetSelected(settings.BuildingShape)

	// Create sliders for building shape ratios (visible only when "mixed" is selected)
	squareRatioSlider := widget.NewSlider(0, 100)
	circleRatioSlider := widget.NewSlider(0, 100)
	rectangleRatioSlider := widget.NewSlider(0, 100)

	squareRatioLabel := widget.NewLabel(fmt.Sprintf("Squares: %.0f%%", settings.BuildingShapeRatios["squares"]))
	circleRatioLabel := widget.NewLabel(fmt.Sprintf("Circles: %.0f%%", settings.BuildingShapeRatios["circles"]))
	rectangleRatioLabel := widget.NewLabel(fmt.Sprintf("Rectangles: %.0f%%", settings.BuildingShapeRatios["rectangles"]))

	squareRatioSlider.SetValue(settings.BuildingShapeRatios["squares"])
	circleRatioSlider.SetValue(settings.BuildingShapeRatios["circles"])
	rectangleRatioSlider.SetValue(settings.BuildingShapeRatios["rectangles"])

	ratioContainer := container.NewVBox(
		squareRatioLabel,
		squareRatioSlider,
		circleRatioLabel,
		circleRatioSlider,
		rectangleRatioLabel,
		rectangleRatioSlider,
	)

	minBuildingComplexitySlider := newNumericInputSlider(1, 6, float64(settings.MinBuildingComplexity), "%.0f", "Min Building Complexity")
	minBuildingComplexitySlider.entry.OnChanged = func(s string) {
		minBuildingComplexitySlider.validate(s, func(hasError bool) {
			errorStates["minBuildingComplexity"] = hasError
			updateGenerateBtnState()
		})
	}
	minBuildingComplexitySlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := minBuildingComplexitySlider.value.Get()
		settings.MinBuildingComplexity = int(val)
	}))

	maxBuildingComplexitySlider := newNumericInputSlider(1, 6, float64(settings.MaxBuildingComplexity), "%.0f", "Max Building Complexity")
	maxBuildingComplexitySlider.entry.OnChanged = func(s string) {
		maxBuildingComplexitySlider.validate(s, func(hasError bool) {
			errorStates["maxBuildingComplexity"] = hasError
			updateGenerateBtnState()
		})
	}
	maxBuildingComplexitySlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := maxBuildingComplexitySlider.value.Get()
		settings.MaxBuildingComplexity = int(val)
	}))

	buildingComplexityRatioSlider := newNumericInputSlider(0, 100, settings.BuildingComplexityRatio, "%.0f%%", "Building Complexity Ratio")
	buildingComplexityRatioSlider.entry.OnChanged = func(s string) {
		buildingComplexityRatioSlider.validate(s, func(hasError bool) {
			errorStates["buildingComplexityRatio"] = hasError
			updateGenerateBtnState()
		})
	}
	buildingComplexityRatioSlider.value.AddListener(binding.NewDataListener(func() {
		val, _ := buildingComplexityRatioSlider.value.Get()
		settings.BuildingComplexityRatio = val
	}))

	proceduralContainer := container.NewVBox(
		minBuildingComplexitySlider,
		maxBuildingComplexitySlider,
		buildingComplexityRatioSlider,
	)

	updateRatioSliders := func() {
		squareRatioSlider.SetValue(settings.BuildingShapeRatios["squares"])
		circleRatioSlider.SetValue(settings.BuildingShapeRatios["circles"])
		rectangleRatioSlider.SetValue(settings.BuildingShapeRatios["rectangles"])
		squareRatioLabel.SetText(fmt.Sprintf("Squares: %.0f%%", settings.BuildingShapeRatios["squares"]))
		circleRatioLabel.SetText(fmt.Sprintf("Circles: %.0f%%", settings.BuildingShapeRatios["circles"]))
		rectangleRatioLabel.SetText(fmt.Sprintf("Rectangles: %.0f%%", settings.BuildingShapeRatios["rectangles"]))
	}

	squareRatioSlider.OnChanged = func(val float64) {
		if settings.BuildingShape != "mixed" && settings.BuildingShape != "procedural" {
			return
		}
		oldVal := settings.BuildingShapeRatios["squares"]
		diff := val - oldVal
		settings.BuildingShapeRatios["squares"] = val
		settings.BuildingShapeRatios["circles"] -= diff / 2
		settings.BuildingShapeRatios["rectangles"] -= diff / 2

		// Clamp values to 0-100 range
		if settings.BuildingShapeRatios["circles"] < 0 {
			settings.BuildingShapeRatios["rectangles"] += settings.BuildingShapeRatios["circles"]
			settings.BuildingShapeRatios["circles"] = 0
		}
		if settings.BuildingShapeRatios["rectangles"] < 0 {
			settings.BuildingShapeRatios["circles"] += settings.BuildingShapeRatios["rectangles"]
			settings.BuildingShapeRatios["rectangles"] = 0
		}
		if settings.BuildingShapeRatios["circles"] > 100 {
			settings.BuildingShapeRatios["rectangles"] += settings.BuildingShapeRatios["circles"] - 100
			settings.BuildingShapeRatios["circles"] = 100
		}
		if settings.BuildingShapeRatios["rectangles"] > 100 {
			settings.BuildingShapeRatios["circles"] += settings.BuildingShapeRatios["rectangles"] - 100
			settings.BuildingShapeRatios["rectangles"] = 100
		}

		updateRatioSliders()
	}

	circleRatioSlider.OnChanged = func(val float64) {
		if settings.BuildingShape != "mixed" && settings.BuildingShape != "procedural" {
			return
		}
		oldVal := settings.BuildingShapeRatios["circles"]
		diff := val - oldVal
		settings.BuildingShapeRatios["circles"] = val
		settings.BuildingShapeRatios["squares"] -= diff / 2
		settings.BuildingShapeRatios["rectangles"] -= diff / 2
		// Clamp values to 0-100 range
		if settings.BuildingShapeRatios["squares"] < 0 {
			settings.BuildingShapeRatios["rectangles"] += settings.BuildingShapeRatios["squares"]
			settings.BuildingShapeRatios["squares"] = 0
		}
		if settings.BuildingShapeRatios["rectangles"] < 0 {
			settings.BuildingShapeRatios["squares"] += settings.BuildingShapeRatios["rectangles"]
			settings.BuildingShapeRatios["rectangles"] = 0
		}
		if settings.BuildingShapeRatios["squares"] > 100 {
			settings.BuildingShapeRatios["rectangles"] += settings.BuildingShapeRatios["squares"] - 100
			settings.BuildingShapeRatios["squares"] = 100
		}
		if settings.BuildingShapeRatios["rectangles"] > 100 {
			settings.BuildingShapeRatios["squares"] += settings.BuildingShapeRatios["rectangles"] - 100
			settings.BuildingShapeRatios["rectangles"] = 100
		}

		updateRatioSliders()
	}

	rectangleRatioSlider.OnChanged = func(val float64) {
		if settings.BuildingShape != "mixed" && settings.BuildingShape != "procedural" {
			return
		}
		oldVal := settings.BuildingShapeRatios["rectangles"]
		diff := val - oldVal
		settings.BuildingShapeRatios["rectangles"] = val
		settings.BuildingShapeRatios["squares"] -= diff / 2
		settings.BuildingShapeRatios["circles"] -= diff / 2

		// Clamp values to 0-100 range
		if settings.BuildingShapeRatios["squares"] < 0 {
			settings.BuildingShapeRatios["circles"] += settings.BuildingShapeRatios["squares"]
			settings.BuildingShapeRatios["squares"] = 0
		}
		if settings.BuildingShapeRatios["circles"] < 0 {
			settings.BuildingShapeRatios["squares"] += settings.BuildingShapeRatios["circles"]
			settings.BuildingShapeRatios["circles"] = 0
		}
		if settings.BuildingShapeRatios["squares"] > 100 {
			settings.BuildingShapeRatios["circles"] += settings.BuildingShapeRatios["squares"] - 100
			settings.BuildingShapeRatios["squares"] = 100
		}
		if settings.BuildingShapeRatios["circles"] > 100 {
			settings.BuildingShapeRatios["squares"] += settings.BuildingShapeRatios["circles"] - 100
			settings.BuildingShapeRatios["circles"] = 100
		}

		updateRatioSliders()
	}

	buildingShapeSelect.OnChanged = func(s string) {
		settings.BuildingShape = s
		if s == "mixed" {
			proceduralContainer.Hide()
			ratioContainer.Show()
		} else if s == "procedural" {
			proceduralContainer.Show()
			ratioContainer.Show()
		} else {
			proceduralContainer.Hide()
			ratioContainer.Hide()
		}
	}
	// Initial visibility check
	if settings.BuildingShape == "mixed" {
		proceduralContainer.Hide()
		ratioContainer.Show()
	} else if settings.BuildingShape == "procedural" {
		proceduralContainer.Show()
		ratioContainer.Show()
	} else {
		proceduralContainer.Hide()
		ratioContainer.Hide()
	}

	buildingsTab := container.NewTabItem("Buildings", container.NewVBox(
		numBuildingsSlider,
		minBuildingSizeSlider,
		maxBuildingSizeSlider,
		buildingDistributionSlider,
		buildingShapeLabel,
		buildingShapeSelect,
		proceduralContainer,
		ratioContainer,
	))

	imageTab := container.NewTabItem("Image", container.NewVBox(
		widget.NewLabel("Width:"),
		widthEntry, widget.NewLabel("Height:"),
		heightEntry,
		widget.NewLabel("Seed:"),
		seedEntry,
		randomizeBtn,
		generateBtn,
		errorLabel,
		progressContainer,
		widget.NewSeparator(),
		exportCanvasBtn,
		exportHeightmapBtn,
		exportBumpmapBtn,
		exportMasksBtn,
	))
	// Create the main layout using a horizontal split
	tabs := container.NewAppTabs(
		imageTab,
		terrainTab,
		waterTab,
		roadsTab,
		buildingsTab,
	)

	left := container.NewVBox(
		widget.NewLabel("RPG City Maker Reborn"),
		tabs,
	)

	right := container.NewMax()
	var tappableCanvas, tappableHeightmap, tappableBumpmap *tappableImage

	updateRightPanel := func() {
		var top, bottom fyne.CanvasObject
		switch settings.ImageViewState {
		case 1:
			top = tappableCanvas
			bottom = container.NewGridWithColumns(2, tappableHeightmap, tappableBumpmap)
		case 2:
			top = tappableHeightmap
			bottom = container.NewGridWithColumns(2, tappableCanvas, tappableBumpmap)
		case 3:
			top = tappableBumpmap
			bottom = container.NewGridWithColumns(2, tappableCanvas, tappableHeightmap)
		default:
			right.Objects = []fyne.CanvasObject{container.NewGridWithRows(3, tappableCanvas, tappableHeightmap, tappableBumpmap)}
			right.Refresh()
			return
		}
		split := container.NewVSplit(top, bottom)
		split.Offset = 0.8
		right.Objects = []fyne.CanvasObject{split}
		right.Refresh()
	}

	tappableCanvas = newTappableImage(container.NewMax(canvasImg), func() {
		if settings.ImageViewState == 1 {
			settings.ImageViewState = 0
		} else {
			settings.ImageViewState = 1
		}
		updateRightPanel()
	})
	tappableHeightmap = newTappableImage(container.NewMax(heightmapImg), func() {
		if settings.ImageViewState == 2 {
			settings.ImageViewState = 0
		} else {
			settings.ImageViewState = 2
		}
		updateRightPanel()
	})
	tappableBumpmap = newTappableImage(container.NewMax(bumpmapImg), func() {
		if settings.ImageViewState == 3 {
			settings.ImageViewState = 0
		} else {
			settings.ImageViewState = 3
		}
		updateRightPanel()
	})

	updateRightPanel()

	split := container.NewHSplit(
		left,
		right,
	)
	split.SetOffset(0.3)
	// Set the window content and start the application
	w.SetContent(split)
	w.ShowAndRun()
}

// getImageData encodes an image to the specified format and returns the data as a byte buffer.
func getImageData(img image.Image, format string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	var err error
	switch format {
	case "PNG":
		err = png.Encode(buf, img)
	case "JPG":
		err = jpeg.Encode(buf, img, nil)
	case "WEBP":
		err = webp.Encode(buf, img, &webp.Options{Lossless: true})
	}
	return buf, err
}

// showMasksSaveDialog displays a dialog for saving the generated masks.
func showMasksSaveDialog(win fyne.Window, canvasImg, heightmapImg, bumpmapImg image.Image, settings *Settings, lakes [][]image.Point, riverMask, treeMask, roadMask, bridgeMask, buildingMask *PixelMask) {
	// Create UI elements for the save dialog
	fileNameEntry := widget.NewEntry()
	fileNameEntry.SetPlaceHolder("masks_folder")

	formatSelect := widget.NewSelect([]string{"PNG", "JPG", "WEBP"}, nil)
	formatSelect.SetSelected("PNG")

	packageSelect := widget.NewSelect([]string{"Folder", "tar.gz", "zip"}, nil)
	packageSelect.SetSelected("Folder")

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
		widget.NewLabel("Folder Name:"),
		fileNameEntry,
		widget.NewLabel("Format:"),
		formatSelect,
		widget.NewLabel("Packaging:"),
		packageSelect,
	)

	saveDialog := dialog.NewCustom("Export Masks", "Cancel", content, win)

	saveBtn := widget.NewButton("Save", func() {
		folderName := fileNameEntry.Text
		if folderName == "" {
			return
		}
		imgFormat := strings.ToLower(formatSelect.Selected)
		bounds := canvasImg.Bounds()
		maskToGray := func(mask *PixelMask) *image.Gray {
			out := image.NewGray(bounds)
			if mask == nil {
				return out
			}
			for y := 0; y < mask.Height; y++ {
				row := y * mask.Width
				for x := 0; x < mask.Width; x++ {
					if mask.Data[row+x] != 0 {
						out.SetGray(x, y, color.Gray{Y: 255})
					}
				}
			}
			return out
		}

		// Create mask images from the generated data
		lakeMask := image.NewGray(bounds)
		for _, lake := range lakes {
			for _, p := range lake {
				lakeMask.SetGray(p.X, p.Y, color.Gray{Y: 255})
			}
		}
		riverMaskImg := maskToGray(riverMask)
		treeMaskImg := maskToGray(treeMask)
		roadMaskImg := maskToGray(roadMask)
		bridgeMaskImg := maskToGray(bridgeMask)
		buildingMaskImg := maskToGray(buildingMask)

		imagesToSave := map[string]image.Image{
			"canvas." + imgFormat:         canvasImg,
			"heightmap." + imgFormat:      heightmapImg,
			"bump_map." + imgFormat:       bumpmapImg,
			"lakes_mask." + imgFormat:     lakeMask,
			"rivers_mask." + imgFormat:    riverMaskImg,
			"trees_mask." + imgFormat:     treeMaskImg,
			"roads_mask." + imgFormat:     roadMaskImg,
			"bridges_mask." + imgFormat:   bridgeMaskImg,
			"buildings_mask." + imgFormat: buildingMaskImg,
		}
		// Save the images based on the selected packaging option
		switch packageSelect.Selected {
		case "Folder":
			exportPath := filepath.Join(pathLabel.Text, folderName)
			if err := os.MkdirAll(exportPath, 0755); err != nil {
				log.Println("Error creating directory:", err)
				return
			}
			settings.LastExportPath = exportPath
			for name, img := range imagesToSave {
				saveImage(img, filepath.Join(exportPath, name))
			}
		case "tar.gz":
			filePath := filepath.Join(pathLabel.Text, folderName+".tar.gz")
			settings.LastExportPath = filepath.Dir(filePath)
			file, err := os.Create(filePath)
			if err != nil {
				log.Println("Error creating archive:", err)
				return
			}
			defer file.Close()

			gw := gzip.NewWriter(file)
			defer gw.Close()
			tw := tar.NewWriter(gw)
			defer tw.Close()

			for name, img := range imagesToSave {
				buf, err := getImageData(img, formatSelect.Selected)
				if err != nil {
					log.Printf("Error encoding image %s: %v\n", name, err)
					continue
				}
				hdr := &tar.Header{
					Name: name,
					Mode: 0644,
					Size: int64(buf.Len()),
				}
				if err := tw.WriteHeader(hdr); err != nil {
					log.Printf("Error writing tar header for %s: %v\n", name, err)
					continue
				}
				if _, err := tw.Write(buf.Bytes()); err != nil {
					log.Printf("Error writing tar data for %s: %v\n", name, err)
				}
			}
		case "zip":
			filePath := filepath.Join(pathLabel.Text, folderName+".zip")
			settings.LastExportPath = filepath.Dir(filePath)
			file, err := os.Create(filePath)
			if err != nil {
				log.Println("Error creating archive:", err)
				return
			}
			defer file.Close()

			zw := zip.NewWriter(file)
			defer zw.Close()

			for name, img := range imagesToSave {
				buf, err := getImageData(img, formatSelect.Selected)
				if err != nil {
					log.Printf("Error encoding image %s: %v\n", name, err)
					continue
				}
				f, err := zw.Create(name)
				if err != nil {
					log.Printf("Error creating zip entry for %s: %v\n", name, err)
					continue
				}
				if _, err := f.Write(buf.Bytes()); err != nil {
					log.Printf("Error writing zip data for %s: %v\n", name, err)
				}
			}
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

// saveImage saves an image to the specified path.
func saveImage(img image.Image, path string) {
	file, err := os.Create(path)
	if err != nil {
		log.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		err = png.Encode(file, img)
	case ".jpg":
		err = jpeg.Encode(file, img, nil)
	case ".webp":
		err = webp.Encode(file, img, &webp.Options{Lossless: true})
	}
	if err != nil {
		log.Println("Failed to encode image:", err)
	}
}

// showSaveDialog displays a dialog for saving an image.
func showSaveDialog(win fyne.Window, img image.Image, settings *Settings) {
	// Create UI elements for the save dialog
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
		saveImage(img, filePath)
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
