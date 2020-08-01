package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"html/template"

	rice "github.com/GeertJohan/go.rice"
	"github.com/andskur/argon2-hashing"
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

	server        *echo.Echo
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

	// Static assets
	uiAssets := http.FileServer(rice.MustFindBox("ui").HTTPBox())
	wui.server.GET("/assets/*", echo.WrapHandler(uiAssets))
	wui.server.GET("/plugins/*", echo.WrapHandler(uiAssets))
	wui.server.GET("favicon.ico", echo.WrapHandler(uiAssets))

	// Login handler, with POST being for submitting the form
	wui.server.GET("/login", wui.loginHandler)
	wui.server.POST("/login", wui.loginHandler)

	// Logout handler
	wui.server.GET("/logout", wui.logoutHandler)

	// Device management
	wui.server.GET("/devices", wui.devicesHandler, RequireAuthentication)
	wui.server.POST("/devices/:action", wui.devicesHandler, RequireAuthentication)

	// Dashboard
	wui.server.GET("/", wui.dashboardHandler, RequireAuthentication)

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
	errorPage := fmt.Sprintf("%d.html", code)
	if err := c.Render(code, errorPage, nil); err != nil {
		wui.server.DefaultHTTPErrorHandler(err, c)
	}
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

		return c.Redirect(http.StatusFound, "/login")
	}
}

func (wui *WebUI) loginHandler(c echo.Context) error {
	sess, _ := session.Get("session", c)
	if c.Request().Method == "POST" {
		// TODO: Check the login information and create a session
		username := c.FormValue("username")
		password := c.FormValue("password")

		// Attempt to find the user
		var user User
		wui.DB.Where("username = ?", username).First(&user)

		var hasherr error

		// User found
		if user.ID > 0 {
			// Compare the provided password and the hash in the database
			hasherr = argon2.CompareHashAndPassword(user.Password, []byte(password))

			// If no error, they match
			if hasherr == nil {
				// TODO: Store other session information for better security checks, such as the IP or user agent
				sess.Values["username"] = user.Username
				sess.Save(c.Request(), c.Response())
				return c.Redirect(http.StatusSeeOther, "/")
			}
		}

		// If we get this far, either the user was not found, the password didn't match, or there was an error processing the hash

		// If there was a hash error other than a mismatch, throw a different error
		if hasherr != nil && hasherr != argon2.ErrMismatchedHashAndPassword {
			sess.AddFlash(Toastr{
				Type:    "error",
				Message: "There was an error processing the login.",
			}, "_login")
			log.Printf("WEBUI: There was an error when processing the login for %v: %v", username, hasherr)
		} else {
			sess.AddFlash(Toastr{
				Type:    "error",
				Message: "The username and password provided are not valid.",
			}, "_login")
		}
		sess.Save(c.Request(), c.Response())
		return c.Redirect(http.StatusSeeOther, "/login")
	}

	return c.Render(http.StatusOK, "login.html", nil)
}

func (wui *WebUI) logoutHandler(c echo.Context) error {
	// Clear the user session data
	sess, _ := session.Get("session", c)
	delete(sess.Values, "username")
	sess.Save(c.Request(), c.Response())

	// Redirect back to the login page
	return c.Redirect(http.StatusFound, "/login")
}

func (wui *WebUI) dashboardHandler(c echo.Context) error {
	return c.String(http.StatusOK, "Insert fancy dashboard here")
}

func (wui *WebUI) devicesHandler(c echo.Context) error {
	if c.Request().Method == "POST" {
		action := c.Param("action")
		switch action {
		case "create":
			// Return an error if the MAC address is not valid
			if !isValidMACFormat(c.FormValue("macaddress")) {
				return c.String(http.StatusOK, "Invalid MAC address format provided")
			}

			// Build the model
			device := Device{
				MAC:          normalizeMACAddress(c.FormValue("macaddress")),
				DeviceGroups: []DeviceGroup{},
			}

			for _, groupIDString := range c.Request().Form["devicegroups[]"] {
				var group DeviceGroup
				if groupID, err := strconv.ParseUint(groupIDString, 10, 64); err == nil {
					wui.DB.Find(&group, groupID)
					device.DeviceGroups = append(device.DeviceGroups, group)
				}
			}

			log.Println("Groups:")

			// Attempt to add the device
			if err := wui.DB.Create(&device).Error; err != nil {
				return c.String(http.StatusOK, fmt.Sprintf("Error creating entry: %v", err))
			}

			log.Printf("Added recored for %s", device.MAC)
			return c.Redirect(http.StatusSeeOther, "/devices")
		case "update":
			return c.String(http.StatusOK, fmt.Sprintf("Update %s", c.FormValue("id")))
		case "delete":
			return c.String(http.StatusOK, fmt.Sprintf("Delete %s", c.FormValue("id")))
		default:
			return echo.ErrNotFound
		}
	} else {
		// Get the full list of device groups
		var groups []DeviceGroup
		wui.DB.Find(&groups)

		// Get the full list of MAC addresses and preload their associated device groups
		var devices []Device
		wui.DB.Preload("DeviceGroups").Find(&devices)

		err := c.Render(http.StatusOK, "devices.html", map[string]interface{}{
			"Title":   "Device Management",
			"Groups":  groups,
			"Devices": devices,
		})

		if err != nil {
			return c.String(http.StatusOK, err.Error())
		}

		return nil
	}
}
