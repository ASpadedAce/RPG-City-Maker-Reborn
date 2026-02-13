package main

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// numericInputSlider is a custom widget that combines a slider and a text entry for numeric input.
type numericInputSlider struct {
	widget.BaseWidget
	value      binding.Float
	min, max   float64
	slider     *widget.Slider
	entry      *widget.Entry
	format     string
	errorLabel *widget.Label
	label      *widget.Label
}

type numericInputSliderRenderer struct {
	slider       *numericInputSlider
	label        *widget.Label
	entry        *widget.Entry
	sliderWidget *widget.Slider
	errorLabel   *widget.Label
	layout       fyne.Layout
	objects      []fyne.CanvasObject
}

func (r *numericInputSliderRenderer) MinSize() fyne.Size {
	return r.layout.MinSize(r.objects)
}

func (r *numericInputSliderRenderer) Layout(size fyne.Size) {
	r.layout.Layout(r.objects, size)
}

func (r *numericInputSliderRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *numericInputSliderRenderer) Refresh() {
	r.label.SetText(r.slider.label.Text)
}

func (r *numericInputSliderRenderer) Destroy() {}

// newNumericInputSlider creates a new numericInputSlider widget.
func newNumericInputSlider(min, max float64, initialValue float64, format string, labelText string) *numericInputSlider {
	s := &numericInputSlider{
		min:    min,
		max:    max,
		format: format,
	}
	s.ExtendBaseWidget(s)

	s.label = widget.NewLabel(labelText)
	s.value = binding.NewFloat()
	s.value.Set(initialValue)

	s.slider = widget.NewSlider(min, max)
	s.slider.Bind(s.value)

	s.entry = widget.NewEntry()
	s.value.AddListener(binding.NewDataListener(func() {
		val, _ := s.value.Get()
		s.entry.SetText(fmt.Sprintf(s.format, val))
	}))

	s.errorLabel = widget.NewLabel("")
	s.errorLabel.Hide()

	return s
}

// validate checks the text entry for valid numeric input within the defined range.
func (s *numericInputSlider) validate(text string, onError func(bool)) {
	text = strings.TrimSuffix(text, "px")
	text = strings.TrimSuffix(text, "%")
	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		s.errorLabel.SetText("Not a number")
		s.errorLabel.Show()
		onError(true)
		return
	}

	if val < s.min || val > s.max {
		s.errorLabel.SetText(fmt.Sprintf("Out of range (%.0f-%.0f)", s.min, s.max))
		s.errorLabel.Show()
		onError(true)
		return
	}

	s.errorLabel.Hide()
	onError(false)
	s.value.Set(val)
}

// CreateRenderer is a method required by the Fyne toolkit to render the widget.
func (s *numericInputSlider) CreateRenderer() fyne.WidgetRenderer {
	r := &numericInputSliderRenderer{
		slider:       s,
		label:        s.label,
		entry:        s.entry,
		sliderWidget: s.slider,
		errorLabel:   s.errorLabel,
	}

	r.layout = layout.NewGridLayout(1)
	r.objects = []fyne.CanvasObject{
		container.NewGridWithColumns(2, r.label, r.entry),
		r.sliderWidget,
		r.errorLabel,
	}

	return r
}
