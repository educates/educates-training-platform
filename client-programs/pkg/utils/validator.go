package utils

import (
	"errors"
	"net"
)

/**
 * This function is used to validate and resolve an IP address.
 * It is used when creating a new local registry or registry mirror.
 */
func ValidateAndResolveIP(bindIP string) (string, error) {
	if bindIP == "" {
		return "", errors.New("bind ip cannot be empty")
	}

	ip := net.ParseIP(bindIP)
	if ip == nil {
		// Check if bindIP is a valid domain name
		ip, err := net.LookupHost(bindIP)
		if err != nil {
			return "", errors.New("bind ip is not a valid IP address or a domain name that resolves to an IP address")
		}
		return ip[0], nil
	}

	return ip.String(), nil
}
