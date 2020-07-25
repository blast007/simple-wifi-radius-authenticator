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
