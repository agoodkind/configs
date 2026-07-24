//go:build linux

package health

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"goodkind.io/mwan/internal/ifmgr"
	"goodkind.io/mwan/internal/netif"
)

const (
	defaultStateFile         = "/var/run/mwan-health.state"
	defaultPersistStateFile  = "/var/lib/mwan/health-state"
	defaultTimeout           = 2 * time.Second
	defaultInterval          = 10 * time.Second
	defaultPingCount         = 3
	defaultSuccessThreshold  = 2
	defaultFailureThreshold  = 2
	defaultRecoveryThreshold = 2
)

func defaultTargetsV4() []netip.Addr {
	return []netip.Addr{
		netip.MustParseAddr("1.1.1.1"),
		netip.MustParseAddr("8.8.8.8"),
	}
}

func defaultTargetsV6() []netip.Addr {
	return []netip.Addr{
		netip.MustParseAddr("2606:4700:4700::1111"),
		netip.MustParseAddr("2001:4860:4860::8888"),
	}
}

func validateConfig(cfg Config) error {
	var validationError error
	validationError = errors.Join(validationError, validateProbeConfig(cfg))
	validationError = errors.Join(validationError, validateWANs(cfg.WANs))
	return validationError
}

func validateProbeConfig(cfg Config) error {
	var validationError error
	if cfg.StateFile == "" {
		validationError = errors.Join(validationError, errors.New("state_file is required"))
	}
	if cfg.PersistStateFile == "" {
		validationError = errors.Join(
			validationError,
			errors.New("persist_state_file is required"),
		)
	}
	if len(cfg.TargetsV6) == 0 {
		validationError = errors.Join(
			validationError,
			errors.New("at least one targets_v6 entry is required"),
		)
	}
	if len(cfg.TargetsV4) == 0 {
		validationError = errors.Join(
			validationError,
			errors.New("at least one targets_v4 entry is required"),
		)
	}
	if cfg.Timeout <= 0 {
		validationError = errors.Join(validationError, errors.New("timeout must be > 0"))
	}
	if cfg.Interval <= 0 {
		validationError = errors.Join(validationError, errors.New("interval must be > 0"))
	}
	if cfg.PingCount <= 0 {
		validationError = errors.Join(validationError, errors.New("ping_count must be > 0"))
	}
	if cfg.SuccessThreshold <= 0 {
		validationError = errors.Join(
			validationError,
			errors.New("success_threshold must be > 0"),
		)
	}
	if cfg.SuccessThreshold > len(cfg.TargetsV6) ||
		cfg.SuccessThreshold > len(cfg.TargetsV4) {
		validationError = errors.Join(
			validationError,
			errors.New("success_threshold exceeds an address-family target count"),
		)
	}
	if cfg.FailureThreshold <= 0 {
		validationError = errors.Join(
			validationError,
			errors.New("failure_threshold must be > 0"),
		)
	}
	if cfg.RecoveryThreshold <= 0 {
		validationError = errors.Join(
			validationError,
			errors.New("recovery_threshold must be > 0"),
		)
	}
	return validationError
}

func validateWANs(wans []WAN) error {
	var validationError error
	seenNames := make(map[string]bool, len(wans))
	seenIfaces := make(map[string]bool, len(wans))
	for i, wan := range wans {
		if wan.Name == "" {
			validationError = errors.Join(
				validationError,
				fmt.Errorf("wan[%d]: name is required", i),
			)
		}
		if wan.Iface == "" {
			validationError = errors.Join(
				validationError,
				fmt.Errorf("wan[%d] (%s): iface is required", i, wan.Name),
			)
		}
		if seenNames[wan.Name] {
			validationError = errors.Join(
				validationError,
				fmt.Errorf("wan[%d]: duplicate name %q", i, wan.Name),
			)
		}
		if seenIfaces[wan.Iface] {
			validationError = errors.Join(
				validationError,
				fmt.Errorf("wan[%d]: duplicate iface %q", i, wan.Iface),
			)
		}
		seenNames[wan.Name] = true
		seenIfaces[wan.Iface] = true
	}
	return validationError
}

// New applies shell-compatible defaults before constructing the registered
// module so omitted TOML fields still produce the same baseline probe cadence.
func New(cfg ifmgr.ModuleConfig) (ifmgr.Module, error) {
	healthConfig := Config{
		ShadowMode:        true,
		StateFile:         "",
		PersistStateFile:  "",
		TargetsV4:         nil,
		TargetsV6:         nil,
		HTTPURLs:          nil,
		Timeout:           0,
		Interval:          0,
		PingCount:         0,
		SuccessThreshold:  0,
		FailureThreshold:  0,
		RecoveryThreshold: 0,
		WANs:              nil,
	}
	if cfg != nil {
		typedConfig, ok := cfg.(Config)
		if !ok {
			return nil, fmt.Errorf("health: invalid config type %T", cfg)
		}
		healthConfig = typedConfig
	}
	applyDefaults(&healthConfig)
	return &Module{
		BaseModule:       ifmgr.NewBaseModule(moduleName),
		cfg:              healthConfig,
		clock:            nil,
		cycleMu:          sync.Mutex{},
		reconcileMu:      sync.Mutex{},
		reconcilePending: true,
		statuses:         nil,
		probeV4:          netif.Ping4,
		probeV6:          netif.Ping6,
		probeHTTP:        netif.HTTPCheck,
	}, nil
}

func applyDefaults(cfg *Config) {
	if cfg.StateFile == "" {
		cfg.StateFile = defaultStateFile
	}
	if cfg.PersistStateFile == "" {
		cfg.PersistStateFile = defaultPersistStateFile
	}
	if len(cfg.TargetsV4) == 0 {
		cfg.TargetsV4 = defaultTargetsV4()
	}
	if len(cfg.TargetsV6) == 0 {
		cfg.TargetsV6 = defaultTargetsV6()
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.PingCount == 0 {
		cfg.PingCount = defaultPingCount
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = defaultSuccessThreshold
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}
	if cfg.RecoveryThreshold == 0 {
		cfg.RecoveryThreshold = defaultRecoveryThreshold
	}
}

func init() { ifmgr.Register(moduleName, New) }
