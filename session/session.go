package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

var (
	provides = make(map[string]Provider)
)

type (
	// SessionStore contains all data for one session process with specific id.
	SessionStore interface {
		Set(key, value interface{}) error     //set session value
		Get(key interface{}) interface{}      //get session value
		Delete(key interface{}) error         //delete session value
		SessionID() string                    //back current sessionID
		SessionRelease(w http.ResponseWriter) //release the resource & save data to provider & return the data
		Flush() error                         //delete all data
	}

	// Provider contains global session methods and saved SessionStores.
	// it can operate a SessionStore by its id.
	Provider interface {
		SessionInit(gclifetime int64, config string) error
		SessionRead(sid string) (SessionStore, error)
		SessionExist(sid string) bool
		SessionRegenerate(oldsid, sid string) (SessionStore, error)
		SessionDestroy(sid string) error
		SessionAll() int //get all active session
		SessionGC()
	}

	// Manager contains Provider and its configuration.
	Manager struct {
		provider Provider
		config   *managerConfig
	}

	managerConfig struct {
		CookieName      string `json:"cookieName"`
		EnableSetCookie bool   `json:"enableSetCookie,omitempty"`
		Gclifetime      int64  `json:"gclifetime"`
		Maxlifetime     int64  `json:"maxLifetime"`
		Secure          bool   `json:"secure"`
		CookieLifeTime  int    `json:"cookieLifeTime"`
		ProviderConfig  string `json:"providerConfig"`
		Domain          string `json:"domain"`
		SessionIdLength int64  `json:"sessionIdLength"`
	}
)

// Register makes a session provide available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, provide Provider) {
	if provide == nil {
		panic("session: Register provide is nil")
	}
	if _, dup := provides[name]; dup {
		panic("session: Register called twice for provider " + name)
	}
	provides[name] = provide
}

// Create new Manager with provider name and json config string
// where provider is an existing valid provider(e.g. "cookie") and a
// json config.
func NewManager(provideName, config string) (*Manager, error) {
	provider, ok := provides[provideName]
	if !ok {
		return nil, fmt.Errorf("session: unknown provide: %q (forgotten import?)", provideName)
	}
	cf := new(managerConfig)
	cf.EnableSetCookie = true
	err := json.Unmarshal([]byte(config), cf)
	if err != nil {
		return nil, err
	}
	if cf.Maxlifetime == 0 {
		cf.Maxlifetime = cf.Gclifetime
	}
	err = provider.SessionInit(cf.Maxlifetime, cf.ProviderConfig)
	if err != nil {
		return nil, err
	}

	if cf.SessionIdLength == 0 {
		cf.SessionIdLength = 16
	}

	return &Manager{
		provider,
		cf,
	}, nil
}

// Start session. generate or read the session id from http request.
// if session id exists, return SessionStore with this id.
func (manager *Manager) SessionStart(w http.ResponseWriter, r *http.Request) (session SessionStore, err error) {
	cookie, errs := r.Cookie(manager.config.CookieName)
	if errs != nil || cookie.Value == "" {
		sid, errs := manager.sessionId(r)
		if errs != nil {
			return nil, errs
		}
		session, err = manager.provider.SessionRead(sid)
		cookie = &http.Cookie{Name: manager.config.CookieName,
			Value:    url.QueryEscape(sid),
			Path:     "/",
			HttpOnly: true,
			Secure:   manager.config.Secure,
			Domain:   manager.config.Domain}
		if manager.config.CookieLifeTime >= 0 {
			cookie.MaxAge = manager.config.CookieLifeTime
		}
		if manager.config.EnableSetCookie {
			http.SetCookie(w, cookie)
		}
		r.AddCookie(cookie)
	} else {
		sid, errs := url.QueryUnescape(cookie.Value)
		if errs != nil {
			return nil, errs
		}
		if manager.provider.SessionExist(sid) {
			session, err = manager.provider.SessionRead(sid)
		} else {
			sid, err = manager.sessionId(r)
			if err != nil {
				return nil, errs
			}
			session, err = manager.provider.SessionRead(sid)
			cookie = &http.Cookie{Name: manager.config.CookieName,
				Value:    url.QueryEscape(sid),
				Path:     "/",
				HttpOnly: true,
				Secure:   manager.config.Secure,
				Domain:   manager.config.Domain}
			if manager.config.CookieLifeTime >= 0 {
				cookie.MaxAge = manager.config.CookieLifeTime
			}
			if manager.config.EnableSetCookie {
				http.SetCookie(w, cookie)
			}
			r.AddCookie(cookie)
		}
	}
	return
}

// Destroy session by its id in http request cookie.
func (manager *Manager) SessionDestroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(manager.config.CookieName)
	if err != nil || cookie.Value == "" {
		return
	} else {
		manager.provider.SessionDestroy(cookie.Value)
		expiration := time.Now()
		cookie := http.Cookie{Name: manager.config.CookieName,
			Path:     "/",
			HttpOnly: true,
			Expires:  expiration,
			MaxAge:   -1}
		http.SetCookie(w, &cookie)
	}
}

// Get SessionStore by its id.
func (manager *Manager) GetSessionStore(sid string) (sessions SessionStore, err error) {
	sessions, err = manager.provider.SessionRead(sid)
	return
}

// Start session gc process.
// it can do gc in times after gc lifetime.
func (manager *Manager) GC() {
	manager.provider.SessionGC()
	time.AfterFunc(time.Duration(manager.config.Gclifetime)*time.Second, func() { manager.GC() })
}

// Regenerate a session id for this SessionStore who's id is saving in http request.
func (manager *Manager) SessionRegenerateId(w http.ResponseWriter, r *http.Request) (session SessionStore) {
	sid, err := manager.sessionId(r)
	if err != nil {
		return
	}
	cookie, err := r.Cookie(manager.config.CookieName)
	if err != nil && cookie.Value == "" {
		session, _ = manager.provider.SessionRead(sid)
		cookie = &http.Cookie{Name: manager.config.CookieName,
			Value:    url.QueryEscape(sid),
			Path:     "/",
			HttpOnly: true,
			Secure:   manager.config.Secure,
			Domain:   manager.config.Domain,
		}
	} else {
		oldsid, _ := url.QueryUnescape(cookie.Value)
		session, _ = manager.provider.SessionRegenerate(oldsid, sid)
		cookie.Value = url.QueryEscape(sid)
		cookie.HttpOnly = true
		cookie.Path = "/"
	}
	if manager.config.CookieLifeTime >= 0 {
		cookie.MaxAge = manager.config.CookieLifeTime
	}
	http.SetCookie(w, cookie)
	r.AddCookie(cookie)
	return
}

// Get all active sessions count number.
func (manager *Manager) GetActiveSession() int {
	return manager.provider.SessionAll()
}

// Set cookie with https.
func (manager *Manager) SetSecure(secure bool) {
	manager.config.Secure = secure
}

// generate session id with rand string, unix nano time, remote addr by hash function.
func (manager *Manager) sessionId(r *http.Request) (sid string, err error) {
	b := make([]byte, manager.config.SessionIdLength)
	n, err := rand.Read(b)
	if n != len(b) || err != nil {
		return "", fmt.Errorf("Could not successfully read from the system CSPRNG.")
	}
	return hex.EncodeToString(b), nil
}
