package main

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/aquilax/go-perlin"
)

const (
	alpha = 2.
	beta  = 2.
	n     = 3
)

func GenerateHeightmap(width, height, octaves int, scale float64, seed int64) image.Image {
	p := perlin.NewPerlin(alpha, beta, n, seed)
	img := image.NewGray(image.Rect(0, 0, width, height))

	if scale == 0 {
		scale = 100.0
	}

	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			var noise float64
			frequency := 1.0
			amplitude := 1.0
			maxAmplitude := 0.0

			for i := 0; i < octaves; i++ {
				noise += p.Noise2D(float64(x)*frequency/scale, float64(y)*frequency/scale) * amplitude
				maxAmplitude += amplitude
				amplitude /= 2.0
				frequency *= 2.0
			}

			noise /= maxAmplitude
			grayColor := uint8((noise + 1) * 127.5)
			img.SetGray(x, y, color.Gray{Y: grayColor})
		}
	}

	return img
}

func ApplyRoughness(heightmap image.Image, roughness float64) image.Image {
	bounds := heightmap.Bounds()
	composite := image.NewRGBA(bounds)
	draw.Draw(composite, bounds, heightmap, image.Point{}, draw.Src)

	alphaValue := 255 - uint8(roughness*2.55)
	overlay := image.NewUniform(color.RGBA{R: 128, G: 128, B: 128, A: alphaValue})
	draw.Draw(composite, bounds, overlay, image.Point{}, draw.Over)

	return composite
}
