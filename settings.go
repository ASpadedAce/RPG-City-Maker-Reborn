package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Settings holds all the user-configurable parameters for map generation.
type Settings struct {
	// Terrain settings
	Detail    float64 `json:"detail"`
	Roughness float64 `json:"roughness"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`

	// Water settings
	Lakes                 int     `json:"lakes"`
	LakeSizeLower         float64 `json:"lake_size_lower"`
	LakeSizeUpper         float64 `json:"lake_size_upper"`
	LakeEdgeRoughness     float64 `json:"lake_edge_roughness"`
	LakeShape             string  `json:"lake_shape"`
	Rivers                int     `json:"rivers"`
	MinRiverWidth         float64 `json:"min_river_width"`
	MaxRiverWidth         float64 `json:"max_river_width"`
	RiverCurvyness        float64 `json:"river_curvyness"`
	RiverWidthVariability float64 `json:"river_width_variability"`
	RiverEdgeRoughness    float64 `json:"river_edge_roughness"`

	// Tree settings
	MinTreeSize    float64 `json:"min_tree_size"`
	MaxTreeSize    float64 `json:"max_tree_size"`
	TreeCoverage   float64 `json:"tree_coverage"`
	TreeClumpiness float64 `json:"tree_clumpiness"`

	// Road settings
	MinRoadWidth     float64 `json:"min_road_width"`
	MaxRoadWidth     float64 `json:"max_road_width"`
	BuildingsPerRoad int     `json:"buildings_per_road"`
	RoadExits        int     `json:"road_exits"`
	RoadCurvyness    float64 `json:"road_curvyness"`
	RoadDistribution float64 `json:"road_distribution"`
	MinRoadAngle     float64 `json:"min_road_angle"`

	// Building settings
	NumBuildings         int     `json:"num_buildings"`
	MinBuildingSize      float64 `json:"min_building_size"`
	MaxBuildingSize      float64 `json:"max_building_size"`
	BuildingDistribution float64 `json:"building_distribution"`
	BuildingShape        string  `json:"building_shape"`

	// Ratios for mixed building shapes
	BuildingShapeRatios map[string]float64 `json:"building_shape_ratios"`

	// Procedural building settings
	MinBuildingComplexity   int     `json:"min_building_complexity"`
	MaxBuildingComplexity   int     `json:"max_building_complexity"`
	BuildingComplexityRatio float64 `json:"building_complexity_ratio"`

	// General settings
	Seed           int64  `json:"seed"`
	LastExportPath string `json:"last_export_path"`
	ImageViewState int    `json:"image_view_state"`
}

// Save saves the current settings to a JSON file in the user's config directory.
func (s *Settings) Save() error {
	// Get the user's config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	// Create the application's config directory if it doesn't exist
	appConfigDir := filepath.Join(configDir, "rpgcitymakerreborn")
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		return err
	}

	// Create and open the settings file
	configFile := filepath.Join(appConfigDir, "settings.json")
	file, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Encode the settings as JSON and write to the file
	encoder := json.NewEncoder(file)
	return encoder.Encode(s)
}

// LoadSettings loads the settings from a JSON file in the user's config directory.
// If the file doesn't exist, it returns a default set of settings.
func LoadSettings() (*Settings, error) {
	// Get the user's config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	// Open the settings file
	configFile := filepath.Join(configDir, "rpgcitymakerreborn", "settings.json")
	file, err := os.Open(configFile)
	if err != nil {
		// If the file doesn't exist, return default settings
		if os.IsNotExist(err) {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				homeDir = "."
			}
			return &Settings{
				Detail:                1,
				Roughness:             0,
				Width:                 300,
				Height:                300,
				Lakes:                 0,
				LakeSizeLower:         1,
				LakeSizeUpper:         5,
				LakeEdgeRoughness:     50,
				LakeShape:             "circle",
				MinTreeSize:           1.6,
				MaxTreeSize:           6.6,
				TreeCoverage:          20,
				TreeClumpiness:        50,
				Seed:                  time.Now().UnixNano(),
				Rivers:                0,
				MinRiverWidth:         1,
				MaxRiverWidth:         5,
				RiverCurvyness:        50,
				RiverWidthVariability: 50,
				RiverEdgeRoughness:    50,
				MinRoadWidth:          0.7,
				MaxRoadWidth:          2.7,
				BuildingsPerRoad:      6,
				RoadExits:             5,
				RoadCurvyness:         50,
				RoadDistribution:      50,
				MinRoadAngle:          18,
				NumBuildings:          200,
				MinBuildingSize:       3.5,
				MaxBuildingSize:       10.0,
				BuildingDistribution:  20,
				BuildingShape:         "mixed",
				BuildingShapeRatios: map[string]float64{
					"squares":    40,
					"circles":    30,
					"rectangles": 30,
				},
				MinBuildingComplexity:   1,
				MaxBuildingComplexity:   3,
				BuildingComplexityRatio: 50,
				LastExportPath:          homeDir,
				ImageViewState:          0,
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	// Decode once into Settings, and separately inspect raw keys for field-presence checks.
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	var settings Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	var rawKeys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawKeys); err != nil {
		return nil, err
	}

	if settings.LakeShape == "" {
		settings.LakeShape = "circle"
	}

	// Ensure BuildingShapeRatios is initialized
	if settings.BuildingShapeRatios == nil {
		settings.BuildingShapeRatios = map[string]float64{
			"squares":    40,
			"circles":    30,
			"rectangles": 30,
		}
	}

	// Ensure procedural building settings are initialized
	if settings.MinBuildingComplexity == 0 {
		settings.MinBuildingComplexity = 1
	}
	if settings.MaxBuildingComplexity == 0 {
		settings.MaxBuildingComplexity = 3
	}
	if settings.BuildingComplexityRatio == 0 {
		settings.BuildingComplexityRatio = 50
	}
	if settings.BuildingsPerRoad == 0 {
		settings.BuildingsPerRoad = 6
	}
	if _, ok := rawKeys["min_road_angle"]; !ok {
		settings.MinRoadAngle = 18
	}

	// Tree sizes are percentages of average image dimension.
	// Migrate older pixel-based values when they exceed the valid percentage range.
	if settings.MinTreeSize > maxTreeSizePercent || settings.MaxTreeSize > maxTreeSizePercent {
		avgDim := averageImageDimension(settings.Width, settings.Height)
		if avgDim < 1 {
			avgDim = 1
		}
		settings.MinTreeSize = (settings.MinTreeSize / avgDim) * 100.0
		settings.MaxTreeSize = (settings.MaxTreeSize / avgDim) * 100.0
	}
	settings.MinTreeSize, settings.MaxTreeSize = normalizeTreeSizePercentRange(settings.MinTreeSize, settings.MaxTreeSize)

	// Road widths are percentages of average image dimension.
	// Migrate older pixel-based values when they exceed the valid percentage range.
	if settings.MinRoadWidth > maxRoadWidthPercent || settings.MaxRoadWidth > maxRoadWidthPercent {
		avgDim := averageImageDimension(settings.Width, settings.Height)
		if avgDim < 1 {
			avgDim = 1
		}
		settings.MinRoadWidth = (settings.MinRoadWidth / avgDim) * 100.0
		settings.MaxRoadWidth = (settings.MaxRoadWidth / avgDim) * 100.0
	}
	settings.MinRoadWidth, settings.MaxRoadWidth = normalizeRoadWidthPercentRange(settings.MinRoadWidth, settings.MaxRoadWidth)

	// Building sizes are percentages of average image dimension.
	// Migrate older pixel-based values when they exceed the valid percentage range.
	if settings.MinBuildingSize > maxBuildingSizePercent || settings.MaxBuildingSize > maxBuildingSizePercent {
		avgDim := averageImageDimension(settings.Width, settings.Height)
		if avgDim < 1 {
			avgDim = 1
		}
		settings.MinBuildingSize = (settings.MinBuildingSize / avgDim) * 100.0
		settings.MaxBuildingSize = (settings.MaxBuildingSize / avgDim) * 100.0
	}
	settings.MinBuildingSize, settings.MaxBuildingSize = normalizeBuildingSizePercentRange(settings.MinBuildingSize, settings.MaxBuildingSize)

	// Ensure LastExportPath is set to a default value if it's empty
	if settings.LastExportPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "."
		}
		settings.LastExportPath = homeDir
	}

	return &settings, nil
}
