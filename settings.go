package main

import (
	"encoding/json"
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
	Lakes          int     `json:"lakes"`
	LakeSizeLower  float64 `json:"lake_size_lower"`
	LakeSizeUpper  float64 `json:"lake_size_upper"`
	Rivers         int     `json:"rivers"`
	MinRiverWidth  float64 `json:"min_river_width"`
	MaxRiverWidth  float64 `json:"max_river_width"`
	RiverCurvyness float64 `json:"river_curvyness"`

	// Tree settings
	MinTreeSize    float64 `json:"min_tree_size"`
	MaxTreeSize    float64 `json:"max_tree_size"`
	TreeCoverage   float64 `json:"tree_coverage"`
	TreeClumpiness float64 `json:"tree_clumpiness"`

	// Road settings
	NumRoads         int     `json:"num_roads"`
	MinRoadWidth     float64 `json:"min_road_width"`
	MaxRoadWidth     float64 `json:"max_road_width"`
	RoadExits        int     `json:"road_exits"`
	RoadCurvyness    float64 `json:"road_curvyness"`
	RoadDistribution float64 `json:"road_distribution"`

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
				Detail:               1,
				Roughness:            0,
				Width:                300,
				Height:               300,
				Lakes:                0,
				LakeSizeLower:        1,
				LakeSizeUpper:        5,
				MinTreeSize:          5,
				MaxTreeSize:          20,
				TreeCoverage:         20,
				TreeClumpiness:       50,
				Seed:                 time.Now().UnixNano(),
				Rivers:               0,
				MinRiverWidth:        1,
				MaxRiverWidth:        5,
				RiverCurvyness:       50,
				NumRoads:             100,
				MinRoadWidth:         2,
				MaxRoadWidth:         8,
				RoadExits:            5,
				RoadCurvyness:        50,
				RoadDistribution:     50,
				NumBuildings:         200,
				MinBuildingSize:      10,
				MaxBuildingSize:      30,
				BuildingDistribution: 20,
				BuildingShape:        "mixed",
				BuildingShapeRatios: map[string]float64{
					"squares":    40,
					"circles":    30,
					"rectangles": 30,
				},
				MinBuildingComplexity:   1,
				MaxBuildingComplexity:   3,
				BuildingComplexityRatio: 50,
				LastExportPath:          homeDir,
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	// Decode the JSON data into a Settings struct
	var settings Settings
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&settings); err != nil {
		return nil, err
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
