package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// CheckIDGUIHTTPSResponds is the matrix id for the GUI liveness
// probe. The matching constants below are the matrix ids for the
// remaining web/API checks.
const (
	CheckIDGUIHTTPSResponds = "gui_https_responds"
	// CheckIDAPIFirmwareStatusOK asserts that /api/core/firmware/status returns 200.
	CheckIDAPIFirmwareStatusOK = "api_firmware_status_ok"
	// CheckIDAPIFirmwareRunningVersionMatches asserts product_version starts with the target prefix.
	CheckIDAPIFirmwareRunningVersionMatches = "api_firmware_running_version_matches_target"
	// CheckIDAPIAuthRejectsInvalid asserts the firmware endpoint rejects invalid credentials.
	CheckIDAPIAuthRejectsInvalid = "api_auth_rejects_bad_creds"
	// CheckIDRunningVersionIs26x asserts the running version is 26.x post-upgrade.
	CheckIDRunningVersionIs26x = "running_version_is_26x"
	// CheckIDQuaggaAPIPostOnly asserts the 26.1 POST-only hardening fired on quagga endpoints.
	CheckIDQuaggaAPIPostOnly = "quagga_api_post_only"
)

// guiHTTPSRespondsCheck does a TLS-skipped GET on the OPNsense
// root URL and accepts 200 or 302.
type guiHTTPSRespondsCheck struct{}

// NewGUIHTTPSRespondsCheck returns the GUI-responds check.
func NewGUIHTTPSRespondsCheck() Check { return &guiHTTPSRespondsCheck{} }

func (c *guiHTTPSRespondsCheck) ID() string                   { return CheckIDGUIHTTPSResponds }
func (c *guiHTTPSRespondsCheck) Category() Category           { return CategoryWebAPI }
func (c *guiHTTPSRespondsCheck) Severity() Severity           { return SeverityBlocker }
func (c *guiHTTPSRespondsCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *guiHTTPSRespondsCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.OPNsenseHTTPSGet(ctx, "/", nil)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	res.ParsedValue = strconv.Itoa(out.StatusCode)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	if out.StatusCode == http.StatusOK || out.StatusCode == http.StatusFound {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("https status %d", out.StatusCode)
	return res
}

// apiFirmwareStatusCheck calls /api/core/firmware/status and
// asserts the JSON parses with a `status` field.
type apiFirmwareStatusCheck struct {
	auth *BasicAuth
}

// NewAPIFirmwareStatusCheck returns the firmware-status check.
func NewAPIFirmwareStatusCheck(auth *BasicAuth) Check {
	return &apiFirmwareStatusCheck{auth: auth}
}

func (c *apiFirmwareStatusCheck) ID() string                   { return CheckIDAPIFirmwareStatusOK }
func (c *apiFirmwareStatusCheck) Category() Category           { return CategoryWebAPI }
func (c *apiFirmwareStatusCheck) Severity() Severity           { return SeverityBlocker }
func (c *apiFirmwareStatusCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *apiFirmwareStatusCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.OPNsenseHTTPSGet(ctx, "/api/core/firmware/status", c.auth)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	res.ParsedValue = strconv.Itoa(out.StatusCode)
	if out.StatusCode != http.StatusOK {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("status %d", out.StatusCode)
		return res
	}
	var payload firmwareStatusPayload
	if err := json.Unmarshal([]byte(out.Body), &payload); err != nil {
		res.Outcome = OutcomeError
		res.Message = fmt.Sprintf("decode firmware status: %v", err)
		return res
	}
	if payload.Status == "" {
		res.Outcome = OutcomeFail
		res.Message = "firmware status payload missing 'status' field"
		return res
	}
	res.Outcome = OutcomePass
	return res
}

// firmwareStatusPayload is the typed slice of /api/core/firmware/status
// the validate package depends on. Additional fields exist on the
// wire but are not required for the upgrade-validation matrix.
type firmwareStatusPayload struct {
	Status         string `json:"status"`
	ProductVersion string `json:"product_version"`
}

// apiFirmwareVersionCheck parses product_version from the firmware
// status endpoint and asserts a 26.x prefix.
type apiFirmwareVersionCheck struct {
	auth *BasicAuth
	want string
}

// NewAPIFirmwareVersionCheck returns the version-prefix check.
// wantPrefix defaults to "26." per the matrix.
func NewAPIFirmwareVersionCheck(auth *BasicAuth, wantPrefix string) Check {
	if wantPrefix == "" {
		wantPrefix = "26."
	}
	return &apiFirmwareVersionCheck{auth: auth, want: wantPrefix}
}

func (c *apiFirmwareVersionCheck) ID() string {
	return CheckIDAPIFirmwareRunningVersionMatches
}
func (c *apiFirmwareVersionCheck) Category() Category           { return CategoryWebAPI }
func (c *apiFirmwareVersionCheck) Severity() Severity           { return SeverityBlocker }
func (c *apiFirmwareVersionCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *apiFirmwareVersionCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.OPNsenseHTTPSGet(ctx, "/api/core/firmware/status", c.auth)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	if out.StatusCode != http.StatusOK {
		res.Outcome = OutcomeFail
		res.Message = fmt.Sprintf("status %d", out.StatusCode)
		return res
	}
	var payload firmwareStatusPayload
	if err := json.Unmarshal([]byte(out.Body), &payload); err != nil {
		res.Outcome = OutcomeError
		res.Message = fmt.Sprintf("decode firmware status: %v", err)
		return res
	}
	res.ParsedValue = payload.ProductVersion
	if strings.HasPrefix(payload.ProductVersion, c.want) {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("product_version=%q does not match %q", payload.ProductVersion, c.want)
	return res
}

// apiAuthRejectsBadCredsCheck confirms the firmware endpoint
// rejects bad credentials with 401.
type apiAuthRejectsBadCredsCheck struct{}

// NewAPIAuthRejectsBadCredsCheck returns the auth-reject check.
func NewAPIAuthRejectsBadCredsCheck() Check { return &apiAuthRejectsBadCredsCheck{} }

func (c *apiAuthRejectsBadCredsCheck) ID() string                   { return CheckIDAPIAuthRejectsInvalid }
func (c *apiAuthRejectsBadCredsCheck) Category() Category           { return CategoryWebAPI }
func (c *apiAuthRejectsBadCredsCheck) Severity() Severity           { return SeverityRegression }
func (c *apiAuthRejectsBadCredsCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *apiAuthRejectsBadCredsCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.OPNsenseHTTPSGet(ctx, "/api/core/firmware/status",
		&BasicAuth{Username: "baduser", Password: "badpass"})
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	res.ParsedValue = strconv.Itoa(out.StatusCode)
	if out.StatusCode == http.StatusUnauthorized {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("expected 401 got %d", out.StatusCode)
	return res
}

// quaggaAPIPostOnlyCheck confirms the 26.1 POST-only hardening
// fired by sending a GET to a quagga set endpoint.
type quaggaAPIPostOnlyCheck struct {
	auth *BasicAuth
}

// NewQuaggaAPIPostOnlyCheck returns the POST-only hardening check
// from resolved decision O-5.
func NewQuaggaAPIPostOnlyCheck(auth *BasicAuth) Check {
	return &quaggaAPIPostOnlyCheck{auth: auth}
}

func (c *quaggaAPIPostOnlyCheck) ID() string                   { return CheckIDQuaggaAPIPostOnly }
func (c *quaggaAPIPostOnlyCheck) Category() Category           { return CategoryWebAPI }
func (c *quaggaAPIPostOnlyCheck) Severity() Severity           { return SeverityRegression }
func (c *quaggaAPIPostOnlyCheck) AppliesWhen(_ *Baseline) bool { return true }

func (c *quaggaAPIPostOnlyCheck) Run(ctx context.Context, env Env) Result {
	started := env.Now()
	out, err := env.OPNsenseHTTPSGet(ctx, "/api/quagga/bgp/set", c.auth)
	finished := env.Now()
	res := newResult(c.ID(), c.Category(), c.Severity(), started, finished)
	if err != nil {
		res.Outcome = OutcomeError
		res.Message = err.Error()
		return res
	}
	res.ParsedValue = strconv.Itoa(out.StatusCode)
	if out.StatusCode == http.StatusMethodNotAllowed || out.StatusCode == http.StatusBadRequest {
		res.Outcome = OutcomePass
		return res
	}
	res.Outcome = OutcomeFail
	res.Message = fmt.Sprintf("expected 405/400 got %d", out.StatusCode)
	return res
}
