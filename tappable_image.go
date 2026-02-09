package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

type tappableImage struct {
	widget.BaseWidget
	image    *fyne.Container
	onTapped func()
}

func newTappableImage(img *fyne.Container, tapped func()) *tappableImage {
	ti := &tappableImage{
		image:    img,
		onTapped: tapped,
	}
	ti.ExtendBaseWidget(ti)
	return ti
}

func (t *tappableImage) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.image)
}

func (t *tappableImage) Tapped(*fyne.PointEvent) {
	if t.onTapped != nil {
		t.onTapped()
	}
}
