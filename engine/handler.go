package engine

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	newrelic "github.com/newrelic/go-agent"
	nrgin "github.com/newrelic/go-agent/_integrations/nrgin/v1"
)

// HandlerConfig defines a Handler
type HandlerConfig struct {
	// Page contains the page description
	Page Page
	// Renderer is the component responsible for rendering the responses
	Renderer Renderer
	// ResponseGenerator gets the data required for generating a response
	// it can get it from a static, local source or from a remote api
	// endpoint
	ResponseGenerator ResponseGenerator
	// CacheControl is the Cache-Control string added into the response headers
	// if everything goes ok
	CacheControl string
}

// DefaultHandlerConfig contains the dafult values for a HandlerConfig
var DefaultHandlerConfig = HandlerConfig{
	Page{},
	EmptyRenderer,
	NoopResponse,
	"public, max-age=3600",
}

// Default404StaticHandler is the default static handler for dealing with 404 errors
var Default404StaticHandler = StaticHandler{[]byte(default404Tmpl)}

// Default500StaticHandler is the default static handler for dealing with 500 errors
var Default500StaticHandler = ErrorHandler{[]byte(default500Tmpl), http.StatusInternalServerError}

// NewHandlerConfig creates a HandlerConfig from the given Page definition
func NewHandlerConfig(page Page) HandlerConfig {
	d, err := time.ParseDuration(page.CacheTTL)
	if err != nil {
		d = time.Hour
	}
	cacheTTL := fmt.Sprintf("public, max-age=%d", int(d.Seconds()))

	if page.BackendURLPattern == "" {
		rg := StaticResponseGenerator{page}
		return HandlerConfig{
			page,
			DefaultHandlerConfig.Renderer,
			rg.ResponseGenerator,
			cacheTTL,
		}
	}

	decoder := JSONDecoder
	if page.IsArray {
		decoder = JSONArrayDecoder
	}
	rg := DynamicResponseGenerator{page, CachedClient(page.BackendURLPattern), decoder}

	return HandlerConfig{
		page,
		DefaultHandlerConfig.Renderer,
		rg.ResponseGenerator,
		cacheTTL,
	}
}

// NewHandler creates a Handler with the given configuration. The returned handler will be keeping itself
// subscribed to the latest template updates using the given subscription channel, allowing hot
// template reloads
func NewHandler(cfg HandlerConfig, subscriptionChan chan Subscription) *Handler {
	h := &Handler{
		cfg.Page,
		cfg.Renderer,
		make(chan Renderer),
		subscriptionChan,
		cfg.ResponseGenerator,
		cfg.CacheControl,
	}
	go h.updateRenderer()
	return h
}

// Handler is a struct that combines a renderer and a response generator for handling
// http requests.
//
// The handler is able to keep itself subscribed to the last renderer version to use
// by wrapping its Input channel into a Subscription and sending it through the Subscribe
// channel every time it gets a new Renderer
type Handler struct {
	Page              Page
	Renderer          Renderer
	Input             chan Renderer
	Subscribe         chan Subscription
	ResponseGenerator ResponseGenerator
	CacheControl      string
}

func (h *Handler) updateRenderer() {
	topic := h.Page.Template
	if h.Page.Layout != "" {
		topic = fmt.Sprintf("%s-:-%s", h.Page.Layout, h.Page.Template)
	}
	for {
		h.Subscribe <- Subscription{topic, h.Input}
		h.Renderer = <-h.Input
	}
}

// HandlerFunc handles a gin request rendering the data returned by the response generator.
// If the response generator does not return an error, it adds a Cache-Control header
func (h *Handler) HandlerFunc(c *gin.Context) {
	if newrelicApp != nil {
		nrgin.Transaction(c).SetName(h.Page.Name)
	}
	result, err := h.ResponseGenerator(c)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if newrelicApp != nil {
		defer newrelic.StartSegment(nrgin.Transaction(c), "Render").End()
	}
	c.Header("Cache-Control", h.CacheControl)
	if err := h.Renderer.Render(c.Writer, result); err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
}

// NewStaticHandler creates a StaticHandler using the content of the received path
func NewStaticHandler(path string) (StaticHandler, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println("reading", path, ":", err.Error())
		return StaticHandler{}, err
	}
	return StaticHandler{data}, nil
}

// StaticHandler is a Handler that writes the injected content
type StaticHandler struct {
	Content []byte
}

// HandlerFunc creates a gin handler that does nothing but writing the static content
func (e *StaticHandler) HandlerFunc() gin.HandlerFunc {
	return func(c *gin.Context) {
		if newrelicApp != nil {
			nrgin.Transaction(c).SetName("StaticHandler")
		}
		c.Writer.Write(e.Content)
	}
}

// NewErrorHandler creates a ErrorHandler using the content of the received path
func NewErrorHandler(path string, code int) (ErrorHandler, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println("reading", path, ":", err.Error())
		return ErrorHandler{}, err
	}
	return ErrorHandler{data, code}, nil
}

// ErrorHandler is a Handler that writes the injected content. It's intended to be dispatched
// by the gin special handlers (NoRoute, NoMethod) but they can also be used as regular handlers
type ErrorHandler struct {
	Content   []byte
	ErrorCode int
}

// HandlerFunc is a gin middleware for dealing with some errors
func (e *ErrorHandler) HandlerFunc() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if !c.IsAborted() || c.Writer.Status() != e.ErrorCode {
			return
		}

		c.Writer.Write(e.Content)
	}
}
