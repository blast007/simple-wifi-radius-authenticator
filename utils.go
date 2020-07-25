package main

import (
	"regexp"
	"strings"
)

var stripMACDelimiters = strings.NewReplacer(":", "", "-", "", ".", "")

func normalizeMACAddress(mac string) string {
	return stripMACDelimiters.Replace(strings.ToLower(mac))
}

// isValidMACFormat validates a normalized MAC address
func isValidMACFormat(mac string) bool {
	validFormat, err := regexp.MatchString(`^[0-9a-f]{12}$`, mac)
	return err == nil && validFormat
}

// prettyPrintMACAddress takes a normalized MAC address and makes it presentable for display
func prettyPrintMACAddress(mac string) string {
	if !isValidMACFormat(mac) {
		return ""
	}

	return strings.ToUpper(mac[0:2] + ":" + mac[2:4] + ":" + mac[4:6] + ":" + mac[6:8] + ":" + mac[8:10] + ":" + mac[10:12])
}
