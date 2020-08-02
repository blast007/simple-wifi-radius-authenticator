package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"html/template"

	rice "github.com/GeertJohan/go.rice"
	"github.com/jinzhu/gorm"
	"github.com/labstack/echo/v4"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo-contrib/session"
	"github.com/wader/gormstore"
)

// WebUI runs the HTTP interface
type WebUI struct {
	Addr string
	DB   *gorm.DB

	server         *echo.Echo
	sessionCleanup chan struct{}

	templates     map[string]*template.Template
	templateFuncs template.FuncMap
	templateBox   *rice.Box
}

// NewWebUI creates a new instance of WebUI
func NewWebUI(db *gorm.DB) WebUI {
	webui := WebUI{}
	webui.Addr = ":8081"
	webui.DB = db
	return webui
}

// Toastr stores values to be passed to Toastr.js
type Toastr struct {
	Type    string
	Message string
	Title   string
}

// Start the WebUI server
func (wui *WebUI) Start(wait *sync.WaitGroup) {
	wui.server = echo.New()
	wui.server.HideBanner = true

	// Define common template functions
	wui.templateFuncs = template.FuncMap{
		"deviceContainsGroup": func(device Device, group DeviceGroup) bool {
			for _, devicegroup := range device.DeviceGroups {
				if devicegroup.ID == group.ID {
					return true
				}
			}

			return false
		},
		"prettyPrintMACAddress": prettyPrintMACAddress,
	}

	// Get the template box
	wui.templateBox = rice.MustFindBox("ui/templates")

	// Pre-load a few templates
	wui.templates = map[string]*template.Template{}
	wui.loadTemplate("login.html")
	wui.loadTemplate("dashboard.html")
	wui.loadTemplate("devices.html")

	// Set the template renderer
	wui.server.Renderer = wui

	// Set the HTTP error handler
	wui.server.HTTPErrorHandler = wui.customHTTPErrorHandler

	gob.Register(Toastr{})

	// Set up session middleware
	// TODO: Pull this secret from an environment variable or a configuration file/setting
	store := gormstore.New(wui.DB, []byte("secret"))
	store.SessionOpts = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 5,
		HttpOnly: true,
	}
	wui.server.Use(session.Middleware(store))

	// Set up periodic cleanup of stale sessions
	wui.sessionCleanup = make(chan struct{})
	go store.PeriodicCleanup(1*time.Hour, wui.sessionCleanup)

	// Static assets
	uiAssets := http.FileServer(rice.MustFindBox("ui").HTTPBox())
	wui.server.GET("/assets/*", echo.WrapHandler(uiAssets))
	wui.server.GET("/plugins/*", echo.WrapHandler(uiAssets))
	wui.server.GET("favicon.ico", echo.WrapHandler(uiAssets))

	// Login handler, with POST being for submitting the form
	wui.server.GET("/login", wui.loginHandler).Name = "login"
	wui.server.POST("/login", wui.loginSubmitHandler)

	// Logout handler
	wui.server.GET("/logout", wui.logoutHandler).Name = "logout"

	// Device management
	routeDevices := wui.server.Group("/devices", RequireAuthentication)
	routeDevices.GET("/", wui.devicesHandler).Name = "devices"
	routeDevices.POST("/create", wui.deviceCreateHandler).Name = "device-create"
	routeDevices.POST("/update", wui.deviceUpdateHandler).Name = "device-update"
	routeDevices.POST("/delete", wui.deviceDeleteHandler).Name = "device-delete"

	// Dashboard
	wui.server.GET("/", wui.dashboardHandler, RequireAuthentication).Name = "dashboard"

	go func(wui *WebUI, wait *sync.WaitGroup) {
		log.Printf("WEBUI: Starting server on %v", wui.Addr)

		if err := wui.server.Start(wui.Addr); err != nil && err != http.ErrServerClosed {
			log.Printf("WEBUI: Error starting web server: %v", err)
		} else {
			log.Printf("WEBUI: Stopped server")
		}

		wait.Done()
	}(wui, wait)
}

// Stop the WebUI server
func (wui *WebUI) Stop() error {
	// Stop the session cleanup
	close(wui.sessionCleanup)

	// Wait up to 5 seconds for existing requests to complete
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wui.server.Shutdown(ctx)
}

func (wui *WebUI) loadTemplate(path string) {
	log.Println("WEBUI: Loading template:", path)

	var t *template.Template

	// Attempt to load the contents of the template
	if templateString, err := wui.templateBox.String(path); err != nil {
		log.Panicln("Unable to load template:", path, err)
	} else {
		// Parse the base template and inject additional functions
		t, err = template.New("").Funcs(wui.templateFuncs).Parse(templateString)
		if err != nil {
			log.Panicln("Unable to parse template:", path, err)
		}
	}

	// Load any dependencies
	t = wui.loadTemplateDependencies(t, path, 0)

	// Store the template
	wui.templates[path] = t
}

func (wui *WebUI) loadTemplateDependencies(t *template.Template, path string, depth int) *template.Template {
	if depth >= 10 {
		log.Panicln("WEBUI: loadTemplateDependencies maximum recursion depth reached")
	}

	if templateString, err := wui.templateBox.String(path); err != nil {
		log.Panicln("Unable to load template dependency:", path, err)
	} else {
		// If we're at a depth deeper than 0, we'll load the template
		if depth > 0 {
			log.Println("WEBUI: Loading template dependency:", path)

			// Parse the dependency
			t, err = t.New(path).Parse(templateString)
			if err != nil {
				log.Panicln("Unable to parse template dependency:", path, err)
			}
		}

		// Look for template tags and extract the file paths
		var re = regexp.MustCompile(`{{[[:space:]]*template[[:space:]]*"(.+\.html)"[[:space:]]*.+[[:space:]]*}}`)
		for _, match := range re.FindAllStringSubmatch(templateString, -1) {
			// Recursively load dependencies
			t = wui.loadTemplateDependencies(t, match[1], depth+1)
		}
	}

	return t
}

// Render a template
func (wui *WebUI) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	// If data is nil, make it a map
	if data == nil {
		data = map[string]interface{}{}
	}

	// Get session
	sess, _ := session.Get("session", c)

	// Add global methods if data is a map
	if viewContext, isMap := data.(map[string]interface{}); isMap {
		viewContext["reverse"] = c.Echo().Reverse
		if username, ok := sess.Values["username"]; ok {
			viewContext["username"] = username
		} else {
			viewContext["username"] = ""
		}
		viewContext["flashes"] = sess.Flashes
	}

	if _, loaded := wui.templates[name]; !loaded {
		wui.loadTemplate(name)
	}

	// Execute the template
	err := wui.templates[name].Execute(w, data)

	// Save the session (needed to flush flashes if they've been read)
	sess.Save(c.Request(), c.Response())

	return err
}

func (wui *WebUI) customHTTPErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
	}

	// Only show custom error pages to logged in users
	sess, _ := session.Get("session", c)
	if username, ok := sess.Values["username"].(string); ok && len(username) > 0 {
		errorPage := fmt.Sprintf("%d.html", code)
		// Check if we have a custom error page for this code
		if _, err := wui.templateBox.Open(errorPage); err == nil {
			// Attempt to render the custom error page
			if err := c.Render(code, errorPage, nil); err == nil {
				return
			}
		}
	}

	// Fall back to the default Echo HTTP error handler
	wui.server.DefaultHTTPErrorHandler(err, c)
}

// RequireAuthentication is a middleware that requires valid authentication, or else it redirects to the login page
func RequireAuthentication(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// TODO: Handle XHR requests differently
		sess, _ := session.Get("session", c)
		// Check that the username is set and has a non-zero length
		if username, ok := sess.Values["username"].(string); ok && len(username) > 0 {
			return next(c)
		}

		return c.Redirect(http.StatusFound, c.Echo().Reverse("login"))
	}
}
