package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// TextOverlayProgressBar is a custom widget that displays a progress bar with text overlay.
// This allows showing progress information (e.g., step description) directly on the progress bar.
type TextOverlayProgressBar struct {
	widget.BaseWidget
	progressBar *widget.ProgressBar
	label       *widget.Label
}

// NewTextOverlayProgressBar creates a new TextOverlayProgressBar.
func NewTextOverlayProgressBar() *TextOverlayProgressBar {
	p := &TextOverlayProgressBar{
		progressBar: widget.NewProgressBar(),
		label:       widget.NewLabel(""),
	}
	p.ExtendBaseWidget(p)
	return p
}

// SetValue sets the progress value of the underlying progress bar.
func (p *TextOverlayProgressBar) SetValue(v float64) {
	p.progressBar.SetValue(v)
}

// SetText sets the text to be displayed on the progress bar.
func (p *TextOverlayProgressBar) SetText(text string) {
	p.label.SetText(text)
}

// CreateRenderer returns a new renderer for the widget.
func (p *TextOverlayProgressBar) CreateRenderer() fyne.WidgetRenderer {
	return &textOverlayProgressBarRenderer{
		progressBar: p.progressBar,
		label:       p.label,
		objects:     []fyne.CanvasObject{p.progressBar, p.label},
	}
}

// textOverlayProgressBarRenderer is the renderer for the TextOverlayProgressBar.
// It handles the layout and rendering of the progress bar and the overlay text.
type textOverlayProgressBarRenderer struct {
	progressBar *widget.ProgressBar
	label       *widget.Label
	objects     []fyne.CanvasObject
}

// Layout defines the size and position of the progress bar and the label.
func (r *textOverlayProgressBarRenderer) Layout(size fyne.Size) {
	r.progressBar.Resize(size)
	r.label.Resize(size)
	r.label.Move(fyne.NewPos(0, 0))
}

// MinSize returns the minimum size of the widget.
func (r *textOverlayProgressBarRenderer) MinSize() fyne.Size {
	return r.progressBar.MinSize()
}

// Refresh redraws the widget.
func (r *textOverlayProgressBarRenderer) Refresh() {
	r.progressBar.Refresh()
	r.label.Refresh()
}

// Objects returns the canvas objects that make up the widget.
func (r *textOverlayProgressBarRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

// Destroy is a no-op for this renderer.
func (r *textOverlayProgressBarRenderer) Destroy() {}

// CustomTheme is a custom theme to make the progress bar thinner.
type CustomTheme struct {
	fyne.Theme
}

// NewCustomTheme creates a new CustomTheme.
func NewCustomTheme(theme fyne.Theme) *CustomTheme {
	return &CustomTheme{Theme: theme}
}

// Size returns the size for a given themeable item.
// It overrides the default progress bar height to make it thinner.
func (t *CustomTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == "progressBar.height" {
		return 10
	}
	return t.Theme.Size(name)
}
