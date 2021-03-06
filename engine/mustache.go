package engine

import (
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/cbroglie/mustache"
)

// NewMustacheRendererMap returns a map with all renderers for the declared templates and layouts
// and an error if something went wrong
func NewMustacheRendererMap(cfg Config) (map[string]*MustacheRenderer, error) {
	result := map[string]*MustacheRenderer{}
	for _, section := range []map[string]string{cfg.Templates, cfg.Layouts} {
		for name, path := range section {
			templateFile, err := os.Open(path)
			if err != nil {
				log.Println("reading", path, ":", err.Error())
				return result, err
			}
			renderer, err := NewMustacheRenderer(templateFile)
			templateFile.Close()
			if err != nil {
				log.Println("parsing", path, ":", err.Error())
				return result, err
			}
			result[name] = renderer
		}
	}
	return result, nil
}

// NewMustacheRenderer returns a MustacheRenderer and an error if something went wrong
func NewMustacheRenderer(r io.Reader) (*MustacheRenderer, error) {
	tmpl, err := newMustacheTemplate(r)
	if err != nil {
		return nil, err
	}
	return &MustacheRenderer{tmpl}, nil
}

// MustacheRenderer is a simple mustache renderer with a single mustache template
type MustacheRenderer struct {
	tmpl *mustache.Template
}

// Render implements the renderer interface
func (m MustacheRenderer) Render(w io.Writer, v interface{}) error {
	return m.tmpl.FRender(w, v)
}

// NewLayoutMustacheRenderer returns a LayoutMustacheRenderer and an error if something went wrong
func NewLayoutMustacheRenderer(t, l io.Reader) (*LayoutMustacheRenderer, error) {
	tmpl, err := newMustacheTemplate(t)
	if err != nil {
		return nil, err
	}
	layout, err := newMustacheTemplate(l)
	if err != nil {
		return nil, err
	}
	return &LayoutMustacheRenderer{tmpl, layout}, nil
}

// LayoutMustacheRenderer is a mustache renderer composing a mustache template with a layout
type LayoutMustacheRenderer struct {
	tmpl   *mustache.Template
	layout *mustache.Template
}

// Render implements the renderer interface
func (m LayoutMustacheRenderer) Render(w io.Writer, v interface{}) error {
	return m.tmpl.FRenderInLayout(w, m.layout, v)
}

func newMustacheTemplate(r io.Reader) (*mustache.Template, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return mustache.ParseStringPartials(string(data), customPartialProvider)
}

type partialProvider struct {
	statics mustache.PartialProvider
	dynamc  mustache.PartialProvider
}

func (sp *partialProvider) Get(name string) (string, error) {
	if data, err := sp.statics.Get(name); err == nil && data != "" {
		return data, nil
	}

	return sp.dynamc.Get(name)
}

var (
	partials = map[string]string{
		"api2html/debug": debuggerTmpl,
	}
	customPartialProvider = &partialProvider{
		dynamc:  &mustache.FileProvider{},
		statics: &mustache.StaticProvider{Partials: partials},
	}
)
