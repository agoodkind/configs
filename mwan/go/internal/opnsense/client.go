// Package opnsense wraps the OPNsense REST API for configuring BGP (FRR/Quagga)
// and managing gateways as part of automated MWAN cutover procedures.
//
// The API endpoint patterns are derived from the reinkens/opnsense-go library,
// but we use direct HTTP calls because that library has broken cross-package
// imports (references github.com/browningluke/opnsense-go internally).
package opnsense

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client provides high-level operations against the OPNsense REST API.
type Client struct {
	http *http.Client
	base string // e.g. "https://192.168.1.1"
	auth string // base64-encoded "key:secret"
	log  *slog.Logger
}

// New creates an OPNsense API client.
func New(cfg Config, log *slog.Logger) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}
	return &Client{
		http: &http.Client{Transport: transport},
		base: strings.TrimRight(cfg.URL, "/"),
		auth: base64.StdEncoding.EncodeToString([]byte(cfg.APIKey + ":" + cfg.APISecret)),
		log:  log,
	}
}

// ---------------------------------------------------------------------------
// BGP configuration
// ---------------------------------------------------------------------------

// ConfigureBGP sets up the full BGP configuration on OPNsense: enables FRR,
// sets ASN/router-id, creates route-maps, creates neighbors linked to
// route-maps, and reconfigures the service.
func (c *Client) ConfigureBGP(ctx context.Context, bgpCfg BGPConfig) error {
	// Step 0: Add firewall rules to allow BGP (TCP 179) on WAN interface.
	c.log.Info("opnsense: adding BGP firewall rules")
	if err := c.addBGPFirewallRules(ctx, bgpCfg); err != nil {
		return fmt.Errorf("add BGP firewall rules: %w", err)
	}

	// Step 1: Enable FRR general (datacenter profile, syslog).
	c.log.Info("opnsense: enabling FRR general settings")
	if err := c.setFRRGeneral(ctx); err != nil {
		return fmt.Errorf("enable FRR general: %w", err)
	}

	// Step 2: Enable BGP with ASN, router-id, log-neighbor-changes,
	//         no network-import-check.
	c.log.Info("opnsense: configuring BGP global settings",
		"asn", bgpCfg.ASN, "router_id", bgpCfg.RouterID)
	if err := c.setBGPGlobal(ctx, bgpCfg.ASN, bgpCfg.RouterID); err != nil {
		return fmt.Errorf("set BGP global: %w", err)
	}

	// Step 3: Create route-maps.
	routeMaps := []struct {
		name      string
		localPref int
	}{
		{"PREFER-PRIMARY", 200},
		{"PREFER-BACKUP", 100},
	}
	routeMapUUIDs := make(map[string]string, len(routeMaps))
	for _, rm := range routeMaps {
		c.log.Info("opnsense: creating route-map", "name", rm.name, "local_pref", rm.localPref)
		uuid, err := c.createRouteMap(ctx, rm.name, rm.localPref)
		if err != nil {
			return fmt.Errorf("create route-map %s: %w", rm.name, err)
		}
		routeMapUUIDs[rm.name] = uuid
		c.log.Info("opnsense: route-map created", "name", rm.name, "uuid", uuid)
	}

	// Step 4: Create each neighbor linked to the appropriate route-map UUID.
	for _, n := range bgpCfg.Neighbors {
		rmUUID := routeMapUUIDs[n.RouteMapIn]
		if rmUUID == "" {
			return fmt.Errorf("neighbor %s references unknown route-map %q", n.Address, n.RouteMapIn)
		}
		c.log.Info("opnsense: creating BGP neighbor",
			"address", n.Address, "remote_as", n.RemoteAS,
			"route_map_in", n.RouteMapIn, "route_map_uuid", rmUUID)
		if err := c.createNeighbor(ctx, n, rmUUID); err != nil {
			return fmt.Errorf("create neighbor %s: %w", n.Address, err)
		}
	}

	// Step 5: Reconfigure the Quagga/FRR service.
	c.log.Info("opnsense: reconfiguring FRR service")
	if err := c.reconfigureQuagga(ctx); err != nil {
		return fmt.Errorf("reconfigure quagga: %w", err)
	}

	c.log.Info("opnsense: BGP configuration complete")
	return nil
}

// addBGPFirewallRules adds pass rules for TCP 179 on the WAN interface (IPv4 + IPv6).
func (c *Client) addBGPFirewallRules(ctx context.Context, bgpCfg BGPConfig) error {
	rules := []struct {
		ipproto   string
		sourceNet string
		desc      string
	}{
		{"inet", bgpCfg.FirewallSourceV4, "BGP from MWAN (IPv4)"},
		{"inet6", bgpCfg.FirewallSourceV6, "BGP from MWAN (IPv6)"},
	}

	for _, r := range rules {
		body := map[string]any{
			"rule": map[string]string{
				"enabled":          "1",
				"action":           "pass",
				"interface":        "wan",
				"direction":        "in",
				"ipprotocol":       r.ipproto,
				"protocol":         "TCP",
				"source_net":       r.sourceNet,
				"destination_port": "179",
				"description":      r.desc,
			},
		}
		c.log.Info("opnsense: adding firewall rule", "ipproto", r.ipproto, "source", r.sourceNet)
		if _, err := c.postAdd(ctx, "/firewall/filter/addRule", body); err != nil {
			return fmt.Errorf("add firewall rule (%s): %w", r.ipproto, err)
		}
	}

	// Apply firewall changes
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/firewall/filter/apply", nil, &resp); err != nil {
		return fmt.Errorf("apply firewall rules: %w", err)
	}

	return nil
}

// setFRRGeneral enables the FRR daemon with datacenter profile and syslog.
func (c *Client) setFRRGeneral(ctx context.Context) error {
	body := map[string]any{
		"general": map[string]string{
			"enabled": "1",
			"profile": "datacenter",
			"log":     "1",
			"snmp":    "0",
		},
	}
	return c.postSaved(ctx, "/quagga/general/set", body)
}

// setBGPGlobal enables BGP and sets ASN, router-id, and common options.
func (c *Client) setBGPGlobal(ctx context.Context, asn uint32, routerID string) error {
	body := map[string]any{
		"bgp": map[string]string{
			"enabled":              "1",
			"asnumber":             strconv.FormatUint(uint64(asn), 10),
			"routerid":             routerID,
			"graceful":             "0",
			"logneighborchanges":   "1",
			"networkimportcheck":   "0",
		},
	}
	return c.postSaved(ctx, "/quagga/bgp/set", body)
}

// createRouteMap creates a BGP route-map entry that sets local-preference.
// Returns the UUID of the newly created route-map.
func (c *Client) createRouteMap(ctx context.Context, name string, localPref int) (string, error) {
	body := map[string]any{
		"routemap": map[string]string{
			"enabled":     "1",
			"name":        name,
			"action":      "permit",
			"id":          "10",
			"set":         fmt.Sprintf("local-preference %d", localPref),
			"description": fmt.Sprintf("Set local-pref %d for %s path", localPref, strings.ToLower(name)),
		},
	}
	return c.postAdd(ctx, "/quagga/bgp/addRoutemap", body)
}

// createNeighbor creates a BGP neighbor linked to the given route-map UUID.
func (c *Client) createNeighbor(ctx context.Context, n BGPNeighborConfig, routeMapUUID string) error {
	bfd := "0"
	if n.BFD {
		bfd = "1"
	}
	body := map[string]any{
		"neighbor": map[string]string{
			"enabled":          "1",
			"address":          n.Address,
			"remoteas":         strconv.FormatUint(uint64(n.RemoteAS), 10),
			"keepalive":        strconv.Itoa(n.Keepalive),
			"holddown":         strconv.Itoa(n.Holddown),
			"bfd":              bfd,
			"linkedRoutemapIn": routeMapUUID,
			"description":      n.Description,
		},
	}
	_, err := c.postAdd(ctx, "/quagga/bgp/addNeighbor", body)
	return err
}

// reconfigureQuagga triggers an FRR/Quagga service reconfigure.
func (c *Client) reconfigureQuagga(ctx context.Context) error {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/quagga/service/reconfigure", nil, &resp); err != nil {
		return err
	}
	s := strings.TrimSpace(strings.ToLower(resp.Status))
	if s != "ok" {
		return fmt.Errorf("reconfigure returned status %q", resp.Status)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Gateway operations
// ---------------------------------------------------------------------------

// gatewaySearchResult represents the JSON returned by search_gateway.
type gatewaySearchResult struct {
	Rows []gatewayRow `json:"rows"`
}

type gatewayRow struct {
	UUID     string      `json:"uuid"`
	Name     string      `json:"name"`
	Gateway  string      `json:"gateway"`
	Disabled interface{} `json:"disabled"`
}

func (g gatewayRow) isDisabled() bool {
	switch v := g.Disabled.(type) {
	case bool:
		return v
	case string:
		return v == "1"
	default:
		return false
	}
}

// FindGatewayByName searches for a gateway by name and returns its UUID.
func (c *Client) FindGatewayByName(ctx context.Context, name string) (string, error) {
	var result gatewaySearchResult
	if err := c.doJSON(ctx, http.MethodGet, "/routing/settings/search_gateway", nil, &result); err != nil {
		return "", fmt.Errorf("search gateways: %w", err)
	}
	for _, gw := range result.Rows {
		if gw.Name == name {
			return gw.UUID, nil
		}
	}
	return "", fmt.Errorf("gateway %q not found", name)
}

// FindGatewayByNameWithAddr searches for a gateway by name and returns its UUID and address.
func (c *Client) FindGatewayByNameWithAddr(ctx context.Context, name string) (uuid string, addr string, err error) {
	var result gatewaySearchResult
	if err := c.doJSON(ctx, http.MethodGet, "/routing/settings/search_gateway", nil, &result); err != nil {
		return "", "", fmt.Errorf("search gateways: %w", err)
	}
	for _, gw := range result.Rows {
		if gw.Name == name {
			return gw.UUID, gw.Gateway, nil
		}
	}
	return "", "", fmt.Errorf("gateway %q not found", name)
}

// ForceDownGateway marks a gateway as "force down" via set_gateway API.
// This excludes it from default route selection while keeping it configured.
func (c *Client) ForceDownGateway(ctx context.Context, uuid string) error {
	c.log.Info("opnsense: marking gateway force_down", "uuid", uuid)
	body := map[string]any{
		"gateway_item": map[string]string{
			"force_down": "1",
		},
	}
	var resp struct {
		Result string `json:"result"`
	}
	endpoint := fmt.Sprintf("/routing/settings/set_gateway/%s", uuid)
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return fmt.Errorf("set force_down on %s: %w", uuid, err)
	}
	if resp.Result != "saved" {
		return fmt.Errorf("set force_down on %s: unexpected result %q", uuid, resp.Result)
	}
	return nil
}

// UnforceDownGateway removes the "force down" mark from a gateway (for rollback).
func (c *Client) UnforceDownGateway(ctx context.Context, uuid string) error {
	c.log.Info("opnsense: removing force_down from gateway", "uuid", uuid)
	body := map[string]any{
		"gateway_item": map[string]string{
			"force_down": "0",
		},
	}
	var resp struct {
		Result string `json:"result"`
	}
	endpoint := fmt.Sprintf("/routing/settings/set_gateway/%s", uuid)
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return fmt.Errorf("unset force_down on %s: %w", uuid, err)
	}
	return nil
}

// ReconfigureFRR reloads FRR config without restarting (avoids rc dependency hooks).
func (c *Client) ReconfigureFRR(ctx context.Context) error {
	c.log.Info("opnsense: reloading FRR (reconfigure)")
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/quagga/service/reconfigure", nil, &resp); err != nil {
		return fmt.Errorf("reconfigure FRR: %w", err)
	}
	return nil
}

// StopStartFRR does a clean stop then start of FRR via the quagga API.
// The "restart" action doesn't actually restart running daemons (watchfrr keeps them).
// Stop + start forces a full daemon cycle, clearing zebra's stale kernel route cache.
// Only safe when gateways are force_down (prevents static route reinstallation).
func (c *Client) StopStartFRR(ctx context.Context) error {
	c.log.Info("opnsense: stopping FRR")
	var stopResp struct {
		Response string `json:"response"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/quagga/service/stop", nil, &stopResp); err != nil {
		return fmt.Errorf("stop FRR: %w", err)
	}

	// Brief pause for daemons to fully exit
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
	}

	c.log.Info("opnsense: starting FRR")
	var startResp struct {
		Response string `json:"response"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/quagga/service/start", nil, &startResp); err != nil {
		return fmt.Errorf("start FRR: %w", err)
	}
	return nil
}

// DisableGateway disables an OPNsense gateway by UUID.
func (c *Client) DisableGateway(ctx context.Context, uuid string) error {
	c.log.Info("opnsense: disabling gateway", "uuid", uuid)
	return c.toggleGateway(ctx, uuid, true)
}

// EnableGateway re-enables an OPNsense gateway by UUID.
func (c *Client) EnableGateway(ctx context.Context, uuid string) error {
	c.log.Info("opnsense: enabling gateway", "uuid", uuid)
	return c.toggleGateway(ctx, uuid, false)
}

// toggleGateway enables or disables a gateway via the routing API.
// It reads the current state first to avoid unnecessary toggles.
func (c *Client) toggleGateway(ctx context.Context, uuid string, wantDisabled bool) error {
	// Read current state to decide if we need to toggle.
	var result gatewaySearchResult
	if err := c.doJSON(ctx, http.MethodGet, "/routing/settings/search_gateway", nil, &result); err != nil {
		return fmt.Errorf("search gateways: %w", err)
	}

	var found *gatewayRow
	for i := range result.Rows {
		if result.Rows[i].UUID == uuid {
			found = &result.Rows[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("gateway %s not found", uuid)
	}

	if found.isDisabled() == wantDisabled {
		c.log.Info("opnsense: gateway already in desired state",
			"uuid", uuid, "disabled", found.isDisabled())
		return nil
	}

	// Toggle the gateway.
	endpoint := fmt.Sprintf("/routing/settings/toggle_gateway/%s", uuid)
	var resp struct {
		Result  string `json:"result"`
		Changed bool   `json:"changed"`
	}
	if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, &resp); err != nil {
		return fmt.Errorf("toggle gateway %s: %w", uuid, err)
	}

	c.log.Info("opnsense: gateway toggled", "uuid", uuid, "result", resp.Result)
	return nil
}


// DeleteRoute deletes a route from the kernel via the OPNsense diagnostics API.
// Use destination="default" and gateway=<ip> to delete a default route.
func (c *Client) DeleteRoute(ctx context.Context, destination, gateway string) error {
	c.log.Info("opnsense: deleting route", "destination", destination, "gateway", gateway)

	data := fmt.Sprintf("destination=%s&gateway=%s", destination, gateway)
	var resp struct {
		Message string `json:"message"`
	}
	if err := c.doForm(ctx, "/diagnostics/interface/delRoute", data, &resp); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}

	if resp.Message == "not_found" {
		c.log.Info("opnsense: route not found (already removed)", "destination", destination, "gateway", gateway)
	} else {
		c.log.Info("opnsense: route deleted", "destination", destination, "gateway", gateway, "result", resp.Message)
	}
	return nil
}

// Reconfigure applies pending OPNsense routing changes (gateways, static routes).
func (c *Client) Reconfigure(ctx context.Context) error {
	c.log.Info("opnsense: applying routing reconfiguration")
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/routes/routes/reconfigure", nil, &resp); err != nil {
		return fmt.Errorf("reconfigure routing: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// BGP status
// ---------------------------------------------------------------------------

// BGPSummary holds the parsed BGP summary from OPNsense diagnostics.
type BGPSummary struct {
	IPv4Unicast *BGPAFSummary `json:"ipv4Unicast,omitempty"`
	IPv6Unicast *BGPAFSummary `json:"ipv6Unicast,omitempty"`
}

// BGPAFSummary holds per-address-family BGP summary data.
type BGPAFSummary struct {
	RouterID  string                    `json:"routerId"`
	AS        uint32                    `json:"as"`
	PeerCount int                       `json:"peerCount"`
	RIBCount  int                       `json:"ribCount"`
	Peers     map[string]BGPPeerSummary `json:"peers"`
}

// BGPPeerSummary holds per-peer status from the OPNsense diagnostics API.
type BGPPeerSummary struct {
	Hostname        string `json:"hostname"`
	SoftwareVersion string `json:"softwareVersion"`
	RemoteAS        uint32 `json:"remoteAs"`
	State           string `json:"state"`
	PeerState       string `json:"peerState"`
	PeerUptime      string `json:"peerUptime"`
	PfxRcd          int    `json:"pfxRcd"`
	PfxSnt          int    `json:"pfxSnt"`
}

// GetBGPStatus returns the current BGP neighbor status from OPNsense.
func (c *Client) GetBGPStatus(ctx context.Context) (*BGPSummary, error) {
	var resp struct {
		Response BGPSummary `json:"response"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/quagga/diagnostics/bgpsummary", nil, &resp); err != nil {
		return nil, fmt.Errorf("get BGP status: %w", err)
	}
	return &resp.Response, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// doJSON performs an HTTP request to the OPNsense API and decodes the JSON response.
func (c *Client) doJSON(ctx context.Context, method, endpoint string, body any, resp any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.base + "/api" + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+c.auth)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.log.Debug("opnsense: API request", "method", method, "url", url)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP %s %s: %w", method, endpoint, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("HTTP %s %s: status %d: %s", method, endpoint, res.StatusCode, string(respBody))
	}

	if resp != nil {
		if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
			return fmt.Errorf("decode response from %s: %w", endpoint, err)
		}
	}

	return nil
}

// doForm performs a form-encoded POST to the OPNsense API.
func (c *Client) doForm(ctx context.Context, endpoint, formData string, resp any) error {
	url := c.base + "/api" + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(formData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+c.auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	c.log.Debug("opnsense: API request", "method", "POST", "url", url)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST %s: %w", endpoint, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("HTTP POST %s: status %d: %s", endpoint, res.StatusCode, string(respBody))
	}

	if resp != nil {
		if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
			return fmt.Errorf("decode response from %s: %w", endpoint, err)
		}
	}

	return nil
}

// postSaved sends a POST to OPNsense and validates that result == "saved".
func (c *Client) postSaved(ctx context.Context, endpoint string, body any) error {
	var resp struct {
		Result      string         `json:"result"`
		Validations map[string]any `json:"validations,omitempty"`
	}
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return err
	}
	if resp.Result != "saved" {
		return fmt.Errorf("%s: result=%q validations=%v", endpoint, resp.Result, resp.Validations)
	}
	return nil
}

// postAdd sends a POST to create a resource and returns the UUID.
func (c *Client) postAdd(ctx context.Context, endpoint string, body any) (string, error) {
	var resp struct {
		Result      string         `json:"result"`
		UUID        string         `json:"uuid"`
		Validations map[string]any `json:"validations,omitempty"`
	}
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return "", err
	}
	if resp.Result != "saved" {
		return "", fmt.Errorf("%s: result=%q validations=%v", endpoint, resp.Result, resp.Validations)
	}
	return resp.UUID, nil
}
