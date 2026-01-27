package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Settings struct {
	Detail         float64 `json:"detail"`
	Roughness      float64 `json:"roughness"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Lakes          int     `json:"lakes"`
	LakeSizeLower  float64 `json:"lake_size_lower"`
	LakeSizeUpper  float64 `json:"lake_size_upper"`
	MinTreeSize    float64 `json:"min_tree_size"`
	MaxTreeSize    float64 `json:"max_tree_size"`
	TreeCoverage   float64 `json:"tree_coverage"`
	TreeClumpiness float64 `json:"tree_clumpiness"`
	Seed           int64   `json:"seed"`
}

func (s *Settings) Save() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	appConfigDir := filepath.Join(configDir, "rpgcitymakerreborn")
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		return err
	}

	configFile := filepath.Join(appConfigDir, "settings.json")
	file, err := os.Create(configFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(s)
}

func LoadSettings() (*Settings, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	configFile := filepath.Join(configDir, "rpgcitymakerreborn", "settings.json")
	file, err := os.Open(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{
				Detail:         1,
				Roughness:      0,
				Width:          300,
				Height:         300,
				Lakes:          0,
				LakeSizeLower:  1,
				LakeSizeUpper:  5,
				MinTreeSize:    5,
				MaxTreeSize:    20,
				TreeCoverage:   20,
				TreeClumpiness: 50,
				Seed:           time.Now().UnixNano(),
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	var settings Settings
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&settings); err != nil {
		return nil, err
	}
	return &settings, nil
}
