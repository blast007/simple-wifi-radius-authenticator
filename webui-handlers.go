package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/andskur/argon2-hashing"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

func (wui *WebUI) loginHandler(c echo.Context) error {
	return c.Render(http.StatusOK, "login.html", nil)
}

func (wui *WebUI) loginSubmitHandler(c echo.Context) error {
	sess, _ := session.Get("session", c)

	// Fetch the information from the form
	username := c.FormValue("username")
	password := c.FormValue("password")

	// Attempt to find the user
	var user User
	var hasherr error
	if !wui.DB.Where("username = ?", username).First(&user).RecordNotFound() {
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

func (wui *WebUI) logoutHandler(c echo.Context) error {
	// Clear the user session data
	sess, _ := session.Get("session", c)
	delete(sess.Values, "username")
	sess.Save(c.Request(), c.Response())

	// Redirect back to the login page
	return c.Redirect(http.StatusFound, "/login")
}

/***********\
* Dashboard *
\***********/

func (wui *WebUI) dashboardHandler(c echo.Context) error {
	return c.String(http.StatusOK, "Insert fancy dashboard here")
}

/*******************\
* Device Management *
\*******************/

func (wui *WebUI) devicesHandler(c echo.Context) error {
	// Get the full list of MAC addresses and preload their associated device groups
	var devices []Device
	wui.DB.Preload("DeviceGroups").Find(&devices)

	// Get the full list of device groups
	var groups []DeviceGroup
	wui.DB.Find(&groups)

	err := c.Render(http.StatusOK, "devices.html", map[string]interface{}{
		"Title":   "Device Management",
		"Devices": devices,
		"Groups":  groups,
	})

	if err != nil {
		return c.String(http.StatusOK, err.Error())
	}

	return nil
}

func (wui *WebUI) deviceCreateHandler(c echo.Context) error {
	// Build the model
	device := Device{
		MAC:          normalizeMACAddress(c.FormValue("macaddress")),
		DeviceGroups: []DeviceGroup{},
	}

	// Return an error if the MAC address is not valid
	if !isValidMACFormat(device.MAC) {
		return c.String(http.StatusOK, "WEBUI: Invalid MAC address format provided")
	}

	// For each group, convert the string ID to an unsigned int, fetch the record, and add it
	for _, groupIDString := range c.Request().Form["devicegroups[]"] {
		var group DeviceGroup
		if groupID, err := strconv.ParseUint(groupIDString, 10, 64); err == nil {
			wui.DB.Find(&group, groupID)
			device.DeviceGroups = append(device.DeviceGroups, group)
		}
	}

	// Attempt to add the device
	if err := wui.DB.Create(&device).Error; err != nil {
		return c.String(http.StatusOK, fmt.Sprintf("Error creating entry: %v", err))
	}

	log.Printf("WEBUI: Added Device record for %s", prettyPrintMACAddress(device.MAC))
	return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("devices"))
}

func (wui *WebUI) deviceUpdateHandler(c echo.Context) error {
	var id = c.FormValue("id")
	var device Device
	var response Toastr

	// Fetch the record and handle if it doesn't exist
	if wui.DB.First(&device, id).RecordNotFound() {
		response = Toastr{
			Message: fmt.Sprintf("Device with ID of %v was not found.", id),
			Type:    "error",
		}
	} else {
		// For each group, convert the string ID to an unsigned int, fetch the record, and add it
		for _, groupIDString := range c.Request().Form["devicegroups[]"] {
			var group DeviceGroup
			if groupID, err := strconv.ParseUint(groupIDString, 10, 64); err == nil {
				wui.DB.Find(&group, groupID)
				device.DeviceGroups = append(device.DeviceGroups, group)
			}
		}

		// Save the record
		if err := wui.DB.Save(&device).Error; err != nil {
			response = Toastr{
				Message: fmt.Sprintf("Error updating device %v.", prettyPrintMACAddress(device.MAC)),
				Type:    "error",
			}
			log.Println("WEBUI: Error updating device", prettyPrintMACAddress(device.MAC), err)
		} else {
			response = Toastr{
				Message: fmt.Sprintf("Device %v has been updated.", prettyPrintMACAddress(device.MAC)),
				Type:    "success",
			}
		}
	}

	return c.JSON(http.StatusOK, response)
}

func (wui *WebUI) deviceDeleteHandler(c echo.Context) error {
	var id = c.FormValue("id")
	var device Device
	var response Toastr

	// Fetch the record and handle if it doesn't exist
	if wui.DB.First(&device, id).RecordNotFound() {
		response = Toastr{
			Message: fmt.Sprintf("Device with ID of %v was not found.", id),
			Type:    "error",
		}
	} else {
		if err := wui.DB.Delete(&device).Error; err != nil {
			response = Toastr{
				Message: fmt.Sprintf("Error deleting device %v.", prettyPrintMACAddress(device.MAC)),
				Type:    "error",
			}
			log.Println("WEBUI: Error deleting device", prettyPrintMACAddress(device.MAC), err)
		} else {
			response = Toastr{
				Message: fmt.Sprintf("Device %v has been deleted.", prettyPrintMACAddress(device.MAC)),
				Type:    "success",
			}
		}
	}

	sess, _ := session.Get("session", c)
	sess.AddFlash(response)
	sess.Save(c.Request(), c.Response())

	return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("devices"))
}

/******************\
* Group Management *
\******************/

func (wui *WebUI) groupsHandler(c echo.Context) error {
	// Get the full list of groups and preload their associated networks
	var groups []DeviceGroup
	wui.DB.Preload("Networks").Find(&groups)

	// Get the full list of networks
	var networks []Network
	wui.DB.Find(&networks)

	err := c.Render(http.StatusOK, "groups.html", map[string]interface{}{
		"Title":    "Group Management",
		"Groups":   groups,
		"Networks": networks,
	})

	if err != nil {
		return c.String(http.StatusOK, err.Error())
	}

	return nil
}

func (wui *WebUI) groupCreateHandler(c echo.Context) error {
	// Build the model
	group := DeviceGroup{
		Name:     c.FormValue("name"),
		Networks: []Network{},
	}

	// For each network, convert the string ID to an unsigned int, fetch the record, and add it
	for _, networkIDString := range c.Request().Form["networks[]"] {
		var network Network
		if networkID, err := strconv.ParseUint(networkIDString, 10, 64); err == nil {
			wui.DB.Find(&network, networkID)
			group.Networks = append(group.Networks, network)
		}
	}

	// Attempt to add the group
	if err := wui.DB.Create(&group).Error; err != nil {
		return c.String(http.StatusOK, fmt.Sprintf("Error creating entry: %v", err))
	}

	log.Printf("WEBUI: Added DeviceGroup record for %s", group.Name)
	return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("groups"))
}

func (wui *WebUI) groupUpdateHandler(c echo.Context) error {
	var id = c.FormValue("id")
	var group DeviceGroup
	var response Toastr

	// Fetch the record and handle if it doesn't exist
	if wui.DB.First(&group, id).RecordNotFound() {
		response = Toastr{
			Message: fmt.Sprintf("Group with ID of %v was not found.", id),
			Type:    "error",
		}
	} else {
		// For each network, convert the string ID to an unsigned int, fetch the record, and add it
		for _, networkIDString := range c.Request().Form["networks[]"] {
			var network Network
			if networkID, err := strconv.ParseUint(networkIDString, 10, 64); err == nil {
				wui.DB.Find(&network, networkID)
				group.Networks = append(group.Networks, network)
			}
		}

		// Save the record
		if err := wui.DB.Save(&group).Error; err != nil {
			response = Toastr{
				Message: fmt.Sprintf("Error updating group %v.", group.Name),
				Type:    "error",
			}
			log.Println("WEBUI: Error updating group", group.Name, err)
		} else {
			response = Toastr{
				Message: fmt.Sprintf("Group %v has been updated.", group.Name),
				Type:    "success",
			}
		}
	}

	return c.JSON(http.StatusOK, response)
}

func (wui *WebUI) groupDeleteHandler(c echo.Context) error {
	var id = c.FormValue("id")
	var group DeviceGroup
	var response Toastr

	// Fetch the record and handle if it doesn't exist
	if wui.DB.First(&group, id).RecordNotFound() {
		response = Toastr{
			Message: fmt.Sprintf("Group with ID of %v was not found.", id),
			Type:    "error",
		}
	} else {
		if err := wui.DB.Delete(&group).Error; err != nil {
			response = Toastr{
				Message: fmt.Sprintf("Error deleting group %v.", group.Name),
				Type:    "error",
			}
			log.Println("WEBUI: Error deleting group", group.Name, err)
		} else {
			response = Toastr{
				Message: fmt.Sprintf("Group %v has been deleted.", group.Name),
				Type:    "success",
			}
		}
	}

	sess, _ := session.Get("session", c)
	sess.AddFlash(response)
	sess.Save(c.Request(), c.Response())

	return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("groups"))
}
