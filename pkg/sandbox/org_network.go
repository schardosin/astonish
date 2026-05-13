package sandbox

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/lxc/incus/v6/shared/api"
)

// OrgNetworkName returns the Incus network bridge name for an organization.
// Each org gets its own isolated bridge: org-{slug}-br0
// This ensures containers from different orgs cannot communicate.
func OrgNetworkName(orgSlug string) string {
	// Incus network names: lowercase alphanumeric + hyphens, max 15 chars
	// for bridge interfaces (Linux kernel limit).
	sanitized := sanitizeInstanceName(orgSlug)
	name := "org-" + sanitized + "-br0"
	// Linux bridge name limit is 15 chars. Truncate the slug if needed.
	if len(name) > 15 {
		// Keep "org-" prefix (4) + "-br0" suffix (4) = 8, leaving 7 for slug
		maxSlug := 15 - 8
		if len(sanitized) > maxSlug {
			sanitized = sanitized[:maxSlug]
		}
		sanitized = strings.TrimRight(sanitized, "-")
		name = "org-" + sanitized + "-br0"
	}
	return name
}

// OrgProfileName returns the Incus profile name for an organization.
// Each org gets a profile that attaches containers to the org's bridge.
func OrgProfileName(orgSlug string) string {
	return "org-" + sanitizeInstanceName(orgSlug)
}

// EnsureOrgNetwork creates the per-org bridge network and profile if they
// don't already exist. This should be called during org provisioning or
// when the first container is created for an org.
//
// The bridge is created with:
//   - IPv4 NAT enabled (containers can reach the internet)
//   - IPv6 disabled
//   - Isolated subnet (auto-assigned)
//
// The profile attaches eth0 to the org bridge and inherits the storage pool
// from the default profile.
func EnsureOrgNetwork(client *IncusClient, orgSlug string) error {
	server := client.Server()
	networkName := OrgNetworkName(orgSlug)
	profileName := OrgProfileName(orgSlug)

	// 1. Ensure the org bridge network exists
	_, _, err := server.GetNetwork(networkName)
	if err != nil {
		// Network doesn't exist — create it
		ipv4Address := "auto"
		if client.platform == PlatformDockerIncus {
			// Inside Docker, use static subnets to avoid conflicts.
			// Each org gets a different /24 based on a hash of the slug.
			ipv4Address = orgSubnet(orgSlug)
		}

		slog.Info("creating org network bridge", "org", orgSlug, "network", networkName, "ipv4", ipv4Address)
		err = server.CreateNetwork(api.NetworksPost{
			Name: networkName,
			Type: "bridge",
			NetworkPut: api.NetworkPut{
				Config: map[string]string{
					"ipv4.address": ipv4Address,
					"ipv4.nat":     "true",
					"ipv6.address": "none",
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create org network %s: %w", networkName, err)
		}
	}

	// 2. Ensure the org profile exists with the org bridge
	_, _, err = server.GetProfile(profileName)
	if err != nil {
		// Profile doesn't exist — create it.
		// Copy the root disk from the default profile.
		defaultProfile, _, profErr := server.GetProfile("default")
		if profErr != nil {
			return fmt.Errorf("failed to read default profile: %w", profErr)
		}

		devices := map[string]map[string]string{
			"eth0": {
				"type":    "nic",
				"network": networkName,
				"name":    "eth0",
			},
		}
		// Copy root disk device from default profile if present
		if rootDev, ok := defaultProfile.Devices["root"]; ok {
			devices["root"] = rootDev
		}

		slog.Info("creating org profile", "org", orgSlug, "profile", profileName, "network", networkName)
		err = server.CreateProfile(api.ProfilesPost{
			Name: profileName,
			ProfilePut: api.ProfilePut{
				Description: fmt.Sprintf("Astonish org: %s (isolated network)", orgSlug),
				Devices:     devices,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create org profile %s: %w", profileName, err)
		}
	}

	return nil
}

// DeleteOrgNetwork removes the per-org bridge network and profile.
// Called during org decommissioning. Errors are logged but not fatal
// since the org DB is the authoritative record.
func DeleteOrgNetwork(client *IncusClient, orgSlug string) {
	server := client.Server()
	profileName := OrgProfileName(orgSlug)
	networkName := OrgNetworkName(orgSlug)

	if err := server.DeleteProfile(profileName); err != nil {
		slog.Warn("failed to delete org profile", "org", orgSlug, "profile", profileName, "error", err)
	}
	if err := server.DeleteNetwork(networkName); err != nil {
		slog.Warn("failed to delete org network", "org", orgSlug, "network", networkName, "error", err)
	}
}

// orgSubnet generates a deterministic /24 subnet for an org on Docker+Incus.
// Uses the 10.100-199.x.0/24 range to avoid conflicts with Docker (172.x)
// and the default incusbr0 (10.99.0.0/24).
func orgSubnet(orgSlug string) string {
	// Simple hash: sum of bytes mod 100, mapped to 10.100-199.x.1/24
	var sum int
	for _, b := range []byte(orgSlug) {
		sum += int(b)
	}
	second := 100 + (sum % 100)
	third := (sum / 100) % 256
	return fmt.Sprintf("10.%d.%d.1/24", second, third)
}
