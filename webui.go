package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"html/template"

	rice "github.com/GeertJohan/go.rice"
	"github.com/jinzhu/gorm"
	"github.com/labstack/echo"
)

// WebUI runs the HTTP interface
type WebUI struct {
	Addr string
	DB   *gorm.DB

	server    *echo.Echo
	templates *template.Template
}

// NewWebUI creates a new instance of WebUI
func NewWebUI(db *gorm.DB) WebUI {
	webui := WebUI{}
	webui.Addr = ":8081"
	webui.DB = db
	return webui
}

// Start the WebUI server
func (wui *WebUI) Start(wait *sync.WaitGroup) {
	wui.server = echo.New()
	wui.server.HideBanner = true

	// Load the templates
	var err error
	wui.templates, err = wui.loadTemplates()
	if err != nil {
		panic(err)
	}

	// Set the template renderer
	wui.server.Renderer = wui

	// Set the HTTP error handler
	wui.server.HTTPErrorHandler = wui.customHTTPErrorHandler

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
	wui.server.GET("/devices", wui.devicesHandler)
	wui.server.POST("/devices/:action", wui.devicesHandler)

	// Dashboard
	wui.server.GET("/", wui.dashboardHandler)

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

func (wui *WebUI) loadTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
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
	t := template.New("")

	templates := rice.MustFindBox("ui/templates")
	if templates.Walk("/", func(path string, info os.FileInfo, walkErr error) error {
		// In case of error accessing a file, return it so we don't panic
		if walkErr != nil {
			log.Println("WEBUI: loadTemplates() - Error accessing", path)
			return walkErr
		}
		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Read the contents of the template
		contents, err := templates.String(path)
		if err != nil {
			log.Println("WEBUI: loadTemplates() - Error reading", path)
			return err
		}

		// Parse the template
		// TODO: See if we can parse this as a file instead of a string
		t, err = t.New(path).Funcs(funcs).Parse(contents)
		if err != nil {
			log.Println("WEBUI: loadTemplates() - Error parsing", path)
			return err
		}

		return nil
	}) != nil {
		panic("WEBUI: loadTemplates() - Error loading the template files")
	}

	return t, nil
}

// Render a template
func (wui *WebUI) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	// Add global methods if data is a map
	if viewContext, isMap := data.(map[string]interface{}); isMap {
		viewContext["reverse"] = c.Echo().Reverse
	}

	return wui.templates.ExecuteTemplate(w, name, data)
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

func (wui *WebUI) loginHandler(c echo.Context) error {
	if c.Request().Method == "POST" {
		// TODO: Check the login information and create a session
		return c.String(http.StatusOK, "Do the login thing")
	}

	return c.Render(http.StatusOK, "login.html", nil)
}

func (wui *WebUI) logoutHandler(c echo.Context) error {
	// TODO: Invalidate/clear the session
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
		var macs []Device
		wui.DB.Preload("DeviceGroups").Find(&macs)

		err := c.Render(http.StatusOK, "devices.html", struct {
			Title   string
			Groups  []DeviceGroup
			Devices []Device
		}{
			"Device Management",
			groups,
			macs,
		})

		if err != nil {
			return c.String(http.StatusOK, err.Error())
		}

		return nil
	}
}
