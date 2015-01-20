package flotilla

import (
	"fmt"
	"net/http"
	"sync"
)

type (
	// The base of running a Flotilla instance is an App struct with a Name,
	// an Env with information specific to running the App, and a chain of
	// Blueprints
	App struct {
		p    sync.Pool
		name string
		*engine
		*Config
		*Env
		*Blueprint
	}
)

// Returns a new App instance with no configuration.
func Empty(name string) *App {
	app := &App{name: name, engine: &engine{}, Env: EmptyEnv()}
	app.p.New = app.newCtx
	return app
}

// Returns a new App with default configuration.
func New(name string, conf ...Configuration) *App {
	app := Empty(name)
	app.BaseEnv()
	app.Config = defaultConfig()
	app.Blueprint = NewBlueprint("/")
	app.STATIC("static")
	app.Configured = false
	app.Configuration = conf
	return app
}

func (a *App) Name() string {
	return a.name
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rslt := a.lookup(r.Method, r.URL.Path)
	ctx := a.getCtx(w, r, rslt)
	rslt.handler(ctx)
	a.putCtx(ctx)
}

func (a *App) Run(addr string) {
	if !a.Configured {
		if err := a.Configure(a.Configuration...); err != nil {
			panic(fmt.Sprintf("[FLOTILLA] app could not be configured properly: %s", err))
		}
	}
	if err := http.ListenAndServe(addr, a); err != nil {
		panic(err)
	}
}
