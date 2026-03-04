# RPG City Maker Reborn

This exists as an archive, future development will be taking place on Codeberg: https://codeberg.org/Grimsace/RPG_City_Maker_Reborn
Pre-release project for generating fantasy town/city maps with configurable terrain, water, roads, buildings, and trees.

## Overview

RPG City Maker Reborn is a Go + Fyne desktop app inspired by the original Roleplaying City Map Generator. It supports large image sizes, deterministic seeded generation, and exporting canvas/heightmap/bump map/masks in modern image formats.

## Current Features

- Deterministic generation with a user-provided seed (same settings + same seed = same output).
- Terrain generation with detail and roughness controls.
- Lakes with multiple shapes (`circle`, `oval`, `procedural`) and edge roughness.
- Rivers with width range, curviness, width variability, and edge roughness.
- Roads with:
  - Widths scaled by image size (percent of average image dimension).
  - Junction angle constraint. (this needs some work)
  - Exit roads.
  - Building-driven road count via `Buildings Per Road`.
  - Bridge generation and reduced probability for repeated bridges over the same water body.
  - Distribution model that transitions from compact/organic to near full-canvas coverage.
- Buildings with:
  - Size scaled by image size (percent of average image dimension).
  - Shapes: `squares`, `circles`, `rectangles`, `mixed`, `procedural`.
  - Mixed/procedural shape-ratio sliders.
  - Procedural complexity controls (`Min`, `Max`, `Complexity Ratio`).
  - Automatic cap of requested building count based on image size and average building size.
- Trees with:
  - Size scaled by image size (percent of average image dimension).
  - Coverage target by map area.
  - Clumpiness controlling distribution style (spread vs clustered), not just raw count.
  - Trees are removed on water, roads, or buildings.
- Export:
  - Canvas, heightmap, bump map exports as `PNG`, `JPG`, `WEBP`.
  - Mask export package as `Folder`, `tar.gz`, or `zip`.
  - Saved masks include lakes, rivers, trees, roads, bridges, and buildings.
- Timeout protection: generation steps for lakes, rivers, roads, buildings, and trees continue with partial results if a step exceeds 1 minute, and the UI shows which steps timed out.

## Settings

All settings below are currently implemented in the UI.

| Setting | Description | Range |
| --- | --- | --- |
| **Detail** | Terrain noise detail level. | `1` to `100` |
| **Roughness** | Visual roughness overlay on terrain. | `0` to `100` |
| **Width** | Output image width in pixels. | `10` to `8192` |
| **Height** | Output image height in pixels. | `10` to `8192` |
| **Seed** | Base random seed for deterministic generation. | Any integer |
| **Lakes** | Number of lakes to generate. | `0` to `15` |
| **Min Lake Size** | Minimum lake size (% of map area heuristic). | `1%` to `100%` |
| **Max Lake Size** | Maximum lake size (% of map area heuristic). | `1%` to `100%` |
| **Lake Edge Roughness** | Irregularity/noise along lake boundaries. | `0%` to `100%` |
| **Lake Shape** | Lake shape mode. | `circle`, `oval`, `procedural` |
| **Rivers** | Number of rivers to generate. | `0` to `5` |
| **Min River Width** | Minimum river width (% of smaller image dimension). | `1%` to `100%` |
| **Max River Width** | Maximum river width (% of smaller image dimension). | `1%` to `100%` |
| **River Curvyness** | River path curviness. | `0%` to `100%` |
| **River Width Variability** | Width fluctuation along rivers. | `0%` to `100%` |
| **River Edge Roughness** | Irregularity/noise along river edges. | `0%` to `100%` |
| **Min Tree Size** | Minimum tree size as % of average image dimension `((width+height)/2)`. | `0.2%` to `15%` in `0.2%` steps |
| **Max Tree Size** | Maximum tree size as % of average image dimension `((width+height)/2)`. | `0.2%` to `15%` in `0.2%` steps |
| **Tree Coverage** | Target coverage of map area by trees. | `1%` to `100%` |
| **Tree Clumpiness** | Distribution style of trees (spread vs clustered forest). | `0%` to `100%` |
| **Min Road Width** | Minimum road width as % of average image dimension `((width+height)/2)`. | `0.1%` to `5%` in `0.1%` steps |
| **Max Road Width** | Maximum road width as % of average image dimension `((width+height)/2)`. | `0.1%` to `5%` in `0.1%` steps |
| **Buildings Per Road** | Number of buildings represented by one internal road segment (lower = more roads). | `1` to `20` |
| **Road Exits** | Number of edge-connected exit roads. | `0` to `100` |
| **Minimum Road Angle** | Minimum allowed angle between roads at a junction. | `0°` to `180°` |
| **Road Curvyness** | Curviness of generated roads. | `0%` to `100%` |
| **Distribution** (Roads tab) | Road POI spread from compact center to broad/full-map coverage. | `0%` to `100%` |
| **Number of Buildings** | Requested building count before fit-cap. | `0` to `10000` |
| **Min Building Size** | Minimum building size as % of average image dimension `((width+height)/2)`. | `0.5%` to `25%` in `0.5%` steps |
| **Max Building Size** | Maximum building size as % of average image dimension `((width+height)/2)`. | `0.5%` to `25%` in `0.5%` steps |
| **Building Distribution** | Placement preference from near roads toward random map-wide placement. | `0%` to `100%` |
| **Building Shape** | Primary building generation mode. | `squares`, `circles`, `rectangles`, `mixed`, `procedural` |
| **Squares / Circles / Rectangles** | Shape ratio sliders used by `mixed` and `procedural` building modes. | `0%` to `100%` each (normalized together) |
| **Min Building Complexity** | Minimum number of shape components in procedural buildings. | `1` to `6` |
| **Max Building Complexity** | Maximum number of shape components in procedural buildings. | `1` to `6` |
| **Building Complexity Ratio** | Chance to use a random complexity between min and max. | `0%` to `100%` |

## Acknowledgments

- Inspired by the original [Roleplaying City Map Generator](https://www.rpglibrary.org/software/rpg_city_map_generator/)
- Thanks to the maintainers of Go, Fyne, and other open-source projects that make this one possible.
