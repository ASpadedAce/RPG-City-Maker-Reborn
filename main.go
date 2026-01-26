package main

import (
	"fmt"
	"log"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("RPG City Maker")
	w.Resize(fyne.NewSize(800, 600))

	settings, err := LoadSettings()
	if err != nil {
		log.Println("Error loading settings:", err)
		// Use default settings if loading fails
		settings = &Settings{Detail: 1, Roughness: 0, Width: 300, Height: 300, Lakes: 0, LakeSize: 1}
	}

	w.SetOnClosed(func() {
		if err := settings.Save(); err != nil {
			log.Println("Error saving settings:", err)
		}
	})

	canvasImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}

	heightmapImg := &canvas.Image{
		FillMode: canvas.ImageFillContain,
	}

	// Initial image generation
	noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail))
	canvasWithLakes, lakePixels := GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSize)
	darkenedHeightmap := DarkenLakeAreas(noiseImg, lakePixels)
	compositeImg := ApplyRoughness(darkenedHeightmap, settings.Roughness)
	heightmapImg.Image = compositeImg
	canvasImg.Image = canvasWithLakes

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

	lakeSizeLabel := widget.NewLabel(fmt.Sprintf("Lake Size: %.0f%%", settings.LakeSize))
	lakeSizeSlider := widget.NewSlider(1, 100)
	lakeSizeSlider.OnChanged = func(val float64) {
		settings.LakeSize = val
		lakeSizeLabel.SetText(fmt.Sprintf("Lake Size: %.0f%%", settings.LakeSize))
	}
	lakeSizeSlider.SetValue(settings.LakeSize)

	widthEntry := widget.NewEntry()
	widthEntry.SetText(strconv.Itoa(settings.Width))
	widthEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err == nil {
			settings.Width = val
		}
	}

	heightEntry := widget.NewEntry()
	heightEntry.SetText(strconv.Itoa(settings.Height))
	heightEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err == nil {
			settings.Height = val
		}
	}

	generateBtn := widget.NewButton("Generate", func() {
		noiseImg := GenerateHeightmap(settings.Width, settings.Height, int(settings.Detail))
		canvasWithLakes, lakePixels := GenerateLakes(settings.Width, settings.Height, settings.Lakes, settings.LakeSize)
		darkenedHeightmap := DarkenLakeAreas(noiseImg, lakePixels)
		compositeImg := ApplyRoughness(darkenedHeightmap, settings.Roughness)
		heightmapImg.Image = compositeImg
		heightmapImg.Refresh()
		canvasImg.Image = canvasWithLakes
		canvasImg.Refresh()
	})

	terrainTab := container.NewTabItem("Terrain", container.NewVBox(
		detailLabel,
		detailSlider,
		roughnessLabel,
		roughnessSlider,
		lakesLabel,
		lakesSlider,
		lakeSizeLabel,
		lakeSizeSlider,
	))

	imageTab := container.NewTabItem("Image", container.NewVBox(
		widget.NewLabel("Width:"),
		widthEntry,
		widget.NewLabel("Height:"),
		heightEntry,
		generateBtn,
	))

	tabs := container.NewAppTabs(
		imageTab,
		terrainTab,
	)

	left := container.NewVBox(
		widget.NewLabel("Hello World!"),
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
