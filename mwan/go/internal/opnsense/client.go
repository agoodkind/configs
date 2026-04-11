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
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Disabled string `json:"disabled"`
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

	isDisabled := found.Disabled == "1"
	if isDisabled == wantDisabled {
		c.log.Info("opnsense: gateway already in desired state",
			"uuid", uuid, "disabled", isDisabled)
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

// ---------------------------------------------------------------------------
// BGP status
// ---------------------------------------------------------------------------

// GetBGPStatus returns the current BGP neighbor status from OPNsense.
// This calls the diagnostics endpoint that runs "bgpctl show" under the hood.
func (c *Client) GetBGPStatus(ctx context.Context) (string, error) {
	var resp struct {
		Response string `json:"response"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/quagga/diagnostics/bgpsummary", nil, &resp); err != nil {
		return "", fmt.Errorf("get BGP status: %w", err)
	}
	return resp.Response, nil
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
