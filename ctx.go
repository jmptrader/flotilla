package flotilla

import (
	"errors"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/thrisp/flotilla/engine"
	"github.com/thrisp/flotilla/session"
	"github.com/thrisp/flotilla/xrr"
)

// a Manage function is for managing application and handler context between
// any number of routes, handlers, or application specific functions.
type Manage func(Ctx)

// Ctx is a context interface passing application and handler context.
type Ctx interface {
	Extensor

	// Run is an function that starts and cycles through anything the Ctx needs
	// to do to complete its functionality.
	Run()

	// Next executes the pending handlers, managers, or functions in the chain
	// inside the calling Ctx.
	Next()

	// Cancel is called to finalize the Ctx in any way needed, e.g.
	// post-processing or logging &signalling
	Cancel()
}

var Canceled = errors.New("flotilla.Ctx canceled")

// App.Ctx is the default function for making a default ctx fitting the Ctx interface.
func (a *App) Ctx() MakeCtxFunc {
	return func(rw http.ResponseWriter, rq *http.Request, rs *engine.Result, rt *Route) Ctx {
		c := NewCtx(a.extensions, rs)
		c.reset(rq, rw, rt.managers)
		c.Call("start", a.SessionManager)
		return c
	}
}

type context struct {
	parent   *context
	mu       sync.Mutex
	children map[canceler]bool
	done     chan struct{}
	err      error
	value    *ctx
}

type canceler interface {
	cancel(removeFromParent bool, err error)
	Done() <-chan struct{}
}

func propagateCancel(p *context, child canceler) {
	if p.Done() == nil {
		return
	}
	p.mu.Lock()
	if p.err != nil {
		// parent has already been canceled
		child.cancel(false, p.err)
	} else {
		if p.children == nil {
			p.children = make(map[canceler]bool)
		}
		p.children[child] = true
	}
	p.mu.Unlock()
}

func (c *context) Deadline() (deadline time.Time, ok bool) {
	return
}

func (c *context) Done() <-chan struct{} {
	return c.done
}

func (c *context) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *context) Value(key interface{}) interface{} {
	return c.value
}

func (c *context) cancel(removeFromParent bool, err error) {
	if err == nil {
		panic("Ctx.context: internal error: missing cancel error")
	}
	c.mu.Lock()
	c.err = err
	close(c.done)
	for child := range c.children {
		child.cancel(false, err)
	}
	c.children = nil
	c.mu.Unlock()

	if removeFromParent {
		if c.children != nil {
			delete(c.children, c)
		}
	}
}

type handlers struct {
	index    int8
	managers []Manage
	deferred []Manage
}

func defaulthandlers() *handlers {
	return &handlers{index: -1}
}

type ctx struct {
	*engine.Result
	*context
	*handlers
	xrr.Erroror
	Extensor
	rw      responseWriter
	RW      ResponseWriter
	Request *http.Request
	Session session.SessionStore
	Data    map[string]interface{}
}

func emptyCtx() *ctx {
	return &ctx{handlers: defaulthandlers(), Erroror: xrr.DefaultErroror()}
}

// NewCtx returns a default ctx, given a map of potential Extensions and an
// Engine Result.
func NewCtx(ext map[string]reflect.Value, rs *engine.Result) *ctx {
	c := emptyCtx()
	c.Result = rs
	c.Extensor = newextensor(ext, c)
	c.RW = &c.rw
	return c
}

func (c *ctx) Run() {
	c.push(func(c Ctx) { c.Call("release") })
	c.Next()
	for _, fn := range c.deferred {
		fn(c)
	}
}

func (c *ctx) Next() {
	c.index++
	s := int8(len(c.managers))
	for ; c.index < s; c.index++ {
		c.managers[c.index](c)
	}
}

func (c *ctx) Cancel() {
	c.PostProcess(c.Request, c.RW.Status())
	c.context.cancel(true, Canceled)
}

func (c *ctx) reset(rq *http.Request, rw http.ResponseWriter, m []Manage) {
	c.Request = rq
	c.rw.reset(rw)
	c.context = &context{done: make(chan struct{}), value: c}
	c.handlers = defaulthandlers()
	c.managers = m
}

func (c *ctx) replicate() *ctx {
	child := &context{parent: c.context, done: make(chan struct{}), value: c}
	propagateCancel(c.context, child)
	var rcopy ctx = *c
	rcopy.context = child
	return &rcopy
}

func (c *ctx) rerun(managers ...Manage) {
	if c.index != -1 {
		c.index = -1
	}
	c.managers = managers
	c.Next()
}

func (c *ctx) push(fn Manage) {
	c.deferred = append(c.deferred, fn)
}

// CheckStore is returns a StoreItem and a boolean indicating existence provided
// the current Ctx and a key string.
func CheckStore(c Ctx, key string) (*StoreItem, bool) {
	if item, err := c.Call("store", c, key); err == nil {
		return item.(*StoreItem), true
	}
	return nil, false
}
