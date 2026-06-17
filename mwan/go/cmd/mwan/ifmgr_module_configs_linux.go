//go:build linux

package main

import (
	"fmt"
	"net/netip"
	"time"

	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/ifmgr"
	bridgeprobe "goodkind.io/mwan/internal/ifmgr/modules/bridgeprobe"
	cloudflaredtap "goodkind.io/mwan/internal/ifmgr/modules/cloudflaredtap"
	connprobe "goodkind.io/mwan/internal/ifmgr/modules/connprobe"
	hostipv6policy "goodkind.io/mwan/internal/ifmgr/modules/hostipv6policy"
	mainv4 "goodkind.io/mwan/internal/ifmgr/modules/mainv4"
	oobv4 "goodkind.io/mwan/internal/ifmgr/modules/oobv4"
	oobv6 "goodkind.io/mwan/internal/ifmgr/modules/oobv6"
	policyrules "goodkind.io/mwan/internal/ifmgr/modules/policyrules"
	ralost "goodkind.io/mwan/internal/ifmgr/modules/ralost"
	slaachealth "goodkind.io/mwan/internal/ifmgr/modules/slaachealth"
	wanroutes "goodkind.io/mwan/internal/ifmgr/modules/wanroutes"
	wg "goodkind.io/mwan/internal/ifmgr/modules/wg"
	"goodkind.io/mwan/internal/netif"
)

func buildIfMgrModuleConfigs(
	modules config.IfMgrModulesSection,
) (ifmgr.ModuleConfigSet, error) {
	moduleConfigs := make(ifmgr.ModuleConfigSet)

	wgConfig, err := buildWGConfig(modules.WG)
	if err != nil {
		return nil, err
	}
	moduleConfigs["wg"] = wgConfig

	oobV6Config, err := buildOOBV6Config(modules.OOBV6)
	if err != nil {
		return nil, err
	}
	moduleConfigs["oobv6"] = oobV6Config

	moduleConfigs["oobv4"] = buildOOBV4Config(modules.OOBV4)

	slaacHealthConfig, err := buildSLAACHealthConfig(modules.SLAACHealth)
	if err != nil {
		return nil, err
	}
	moduleConfigs["slaac_health"] = slaacHealthConfig

	raLostConfig, err := buildRALostConfig(modules.RALost)
	if err != nil {
		return nil, err
	}
	moduleConfigs["ra_lost"] = raLostConfig

	connectivityProbeConfig, err := buildConnectivityProbeConfig(modules.ConnectivityProbe)
	if err != nil {
		return nil, err
	}
	moduleConfigs["connectivity_probe"] = connectivityProbeConfig

	bridgeProbeConfig, err := buildBridgeProbeConfig(modules.BridgeProbe)
	if err != nil {
		return nil, err
	}
	moduleConfigs["bridge_probe"] = bridgeProbeConfig

	moduleConfigs["cloudflared_tap"] = buildCloudflaredTapConfig(modules.CloudflaredTap)
	moduleConfigs["mainv4"] = buildMainV4Config(modules.MainV4)

	policyRulesConfig, err := buildPolicyRulesConfig(modules.PolicyRules)
	if err != nil {
		return nil, err
	}
	moduleConfigs["policy_rules"] = policyRulesConfig

	hostIPv6PolicyConfig, err := buildHostIPv6PolicyConfig(modules.HostIPv6Policy)
	if err != nil {
		return nil, err
	}
	moduleConfigs["host_ipv6_policy"] = hostIPv6PolicyConfig

	wanRoutesConfig, err := buildWANRoutesConfig(modules.WANRoutes)
	if err != nil {
		return nil, err
	}
	moduleConfigs["wan_routes"] = wanRoutesConfig

	return moduleConfigs, nil
}

// buildWGConfig returns nil when section is nil so the wg module's
// constructor (wg.New) flips its disabled flag and Init returns the
// ifmgr.ErrModuleDisabled sentinel. A present but defaulted section
// renders local-exec mode on wg0, which is a valid configuration on
// suburban and must NOT trip the disabled sentinel.
func buildWGConfig(section *config.IfMgrWGHealthSection) (ifmgr.ModuleConfig, error) {
	if section == nil {
		return nil, nil
	}
	cfg := wg.Config{
		Iface:             "wg0",
		Sudo:              false,
		WarnHandshakeAge:  180 * time.Second,
		ErrorHandshakeAge: 300 * time.Second,
		Timeout:           10 * time.Second,
		IgnorePeers:       map[string]bool{},
	}
	cfg.SSHHost = section.SSHHost
	if section.SSHPort != nil {
		cfg.SSHPort = *section.SSHPort
	}
	cfg.IdentityFile = section.IdentityFile
	if section.Iface != "" {
		cfg.Iface = section.Iface
	}
	cfg.Sudo = section.Sudo

	var err error
	cfg.WarnHandshakeAge, err = parseDurationSetting(
		section.WarnHandshakeAge,
		cfg.WarnHandshakeAge,
		"ifmgr.modules.wg.warn_handshake_age",
	)
	if err != nil {
		return nil, err
	}
	cfg.ErrorHandshakeAge, err = parseDurationSetting(
		section.ErrorHandshakeAge,
		cfg.ErrorHandshakeAge,
		"ifmgr.modules.wg.error_handshake_age",
	)
	if err != nil {
		return nil, err
	}
	cfg.Timeout, err = parseDurationSetting(
		section.Timeout,
		cfg.Timeout,
		"ifmgr.modules.wg.timeout",
	)
	if err != nil {
		return nil, err
	}
	for _, peer := range section.IgnorePeers {
		cfg.IgnorePeers[peer] = true
	}
	return cfg, nil
}

func buildOOBV6Config(section *config.IfMgrOOBV6Section) (oobv6.Config, error) {
	cfg := oobv6.Config{
		ManageSLAACRule:   true,
		SLAACRulePriority: 7,
	}
	if section == nil {
		return cfg, nil
	}
	cfg.Iface = section.Iface
	cfg.OOBAddr = section.OOBAddr
	cfg.OOBTableID = section.OOBTableID
	if section.ManageSLAACSourceRule != nil {
		cfg.ManageSLAACRule = *section.ManageSLAACSourceRule
	}
	if section.SLAACRulePriority != nil {
		cfg.SLAACRulePriority = *section.SLAACRulePriority
	}
	return cfg, nil
}

func buildOOBV4Config(section *config.IfMgrOOBV4Section) oobv4.Config {
	cfg := oobv4.Config{}
	if section == nil {
		return cfg
	}
	cfg.Iface = section.Iface
	cfg.OOBTableID = section.OOBTableID
	return cfg
}

func buildSLAACHealthConfig(section *config.IfMgrSLAACHealthSection) (slaachealth.Config, error) {
	cfg := slaachealth.Config{
		MaxTogglesPerHour: 4,
		ProbeTimeout:      2 * time.Second,
	}
	if section == nil {
		return cfg, nil
	}
	cfg.Iface = section.Iface

	var err error
	cfg.DegradedAfter, err = parseDurationSetting(
		section.DegradedAfter,
		30*time.Second,
		"ifmgr.modules.slaac_health.degraded_after",
	)
	if err != nil {
		return slaachealth.Config{}, err
	}
	cfg.EscalateAfter, err = parseDurationSetting(
		section.EscalateAfter,
		90*time.Second,
		"ifmgr.modules.slaac_health.escalate_after",
	)
	if err != nil {
		return slaachealth.Config{}, err
	}
	cfg.AlertAfter, err = parseDurationSetting(
		section.AlertAfter,
		300*time.Second,
		"ifmgr.modules.slaac_health.alert_after",
	)
	if err != nil {
		return slaachealth.Config{}, err
	}
	cfg.ProbeTimeout, err = parseDurationSetting(
		section.ProbeTimeout,
		cfg.ProbeTimeout,
		"ifmgr.modules.slaac_health.probe_timeout",
	)
	if err != nil {
		return slaachealth.Config{}, err
	}
	if section.MaxTogglesPerHour != nil {
		cfg.MaxTogglesPerHour = *section.MaxTogglesPerHour
	}
	cfg.ProbeTargetsV6, err = parseAddrList(
		section.ProbeTargetsV6,
		"ifmgr.modules.slaac_health.probe_targets_v6",
	)
	if err != nil {
		return slaachealth.Config{}, err
	}
	return cfg, nil
}

func buildRALostConfig(section *config.IfMgrRALostSection) (ralost.Config, error) {
	cfg := ralost.Config{RALostAfter: 5 * time.Minute}
	if section == nil {
		return cfg, nil
	}
	cfg.Iface = section.Iface
	var err error
	cfg.RALostAfter, err = parseDurationSetting(
		section.RALostAlertAfter,
		cfg.RALostAfter,
		"ifmgr.modules.ra_lost.ra_lost_alert_after",
	)
	if err != nil {
		return ralost.Config{}, err
	}
	return cfg, nil
}

func buildConnectivityProbeConfig(
	section *config.IfMgrConnectivityProbeSection,
) (connprobe.Config, error) {
	cfg := connprobe.Config{
		Timeout:        2 * time.Second,
		UnhealthyAfter: 10 * time.Second,
	}
	if section == nil {
		return cfg, nil
	}
	cfg.Iface = section.Iface

	var err error
	cfg.Timeout, err = parseDurationSetting(
		section.Timeout,
		cfg.Timeout,
		"ifmgr.modules.connectivity_probe.timeout",
	)
	if err != nil {
		return connprobe.Config{}, err
	}
	cfg.UnhealthyAfter, err = parseDurationSetting(
		section.UnhealthyAfter,
		cfg.UnhealthyAfter,
		"ifmgr.modules.connectivity_probe.unhealthy_after",
	)
	if err != nil {
		return connprobe.Config{}, err
	}
	cfg.TargetsV6, err = parseAddrList(
		section.TargetsV6,
		"ifmgr.modules.connectivity_probe.targets_v6",
	)
	if err != nil {
		return connprobe.Config{}, err
	}
	return cfg, nil
}

func buildBridgeProbeConfig(section *config.IfMgrBridgeProbeSection) (bridgeprobe.Config, error) {
	cfg := bridgeprobe.Config{NoSignalAlertAfter: 120 * time.Second}
	if section == nil {
		return cfg, nil
	}
	cfg.Iface = section.Iface
	var err error
	cfg.NoSignalAlertAfter, err = parseDurationSetting(
		section.NoSignalAlertAfter,
		cfg.NoSignalAlertAfter,
		"ifmgr.modules.bridge_probe.no_signal_alert_after",
	)
	if err != nil {
		return bridgeprobe.Config{}, err
	}
	return cfg, nil
}

func buildCloudflaredTapConfig(section *config.IfMgrCloudflaredTapSection) cloudflaredtap.Config {
	cfg := cloudflaredtap.Config{}
	if section == nil {
		return cfg
	}
	cfg.Unit = section.Unit
	cfg.DowngradePatterns = append([]string(nil), section.DowngradePatterns...)
	cfg.JournalctlPath = section.JournalctlPath
	return cfg
}

func buildMainV4Config(section *config.IfMgrMainV4Section) mainv4.Config {
	cfg := mainv4.Config{}
	if section == nil {
		return cfg
	}
	cfg.Iface = section.Iface
	return cfg
}

func buildPolicyRulesConfig(
	section *config.IfMgrPolicyRulesSection,
) (policyrules.Config, error) {
	cfg := policyrules.Config{}
	if section == nil {
		return cfg, nil
	}
	cfg.Rules = make([]netif.DesiredRule, 0, len(section.Rule))
	for i, rule := range section.Rule {
		uidRange, err := buildPolicyRuleUIDRange(rule, lookupUserID)
		if err != nil {
			return policyrules.Config{}, fmt.Errorf(
				"policy_rules.rule[%d]: %w", i, err,
			)
		}
		cfg.Rules = append(cfg.Rules, netif.DesiredRule{
			Family:   rule.Family,
			Priority: rule.Priority,
			From:     rule.From,
			UIDRange: uidRange,
			Table:    rule.Table,
			TableID:  rule.TableID,
		})
	}
	return cfg, nil
}

func buildHostIPv6PolicyConfig(
	section *config.IfMgrHostIPv6PolicySection,
) (hostipv6policy.Config, error) {
	cfg := hostipv6policy.Config{
		MissingIfaceGracePeriod: 2 * time.Minute,
	}
	if section == nil {
		return cfg, nil
	}
	var err error
	cfg.MissingIfaceGracePeriod, err = parseDurationSetting(
		section.MissingIfaceGracePeriod,
		cfg.MissingIfaceGracePeriod,
		"ifmgr.modules.host_ipv6_policy.missing_iface_grace_period",
	)
	if err != nil {
		return hostipv6policy.Config{}, err
	}
	cfg.Policies = make([]hostipv6policy.InterfacePolicy, 0, len(section.Interface))
	for _, policy := range section.Interface {
		cfg.Policies = append(cfg.Policies, hostipv6policy.InterfacePolicy{
			Name:             policy.Name,
			AcceptRA:         policy.AcceptRA,
			AutoConf:         policy.AutoConf,
			AcceptRADefRtr:   policy.AcceptRADefRtr,
			SolicitRA:        policy.SolicitRA,
			CleanupRADefault: policy.CleanupRADefault,
		})
	}
	return cfg, nil
}

func buildWANRoutesConfig(
	section *config.IfMgrWANRoutesSection,
) (wanroutes.Config, error) {
	cfg := wanroutes.Config{}
	if section == nil {
		return cfg, nil
	}
	cfg.InternalIface = section.InternalIface
	cfg.OpnsenseWanLL = section.OpnsenseWanLL
	cfg.OpnsenseEdgeV6 = section.OpnsenseEdgeV6
	cfg.InternalPrefix = section.InternalPrefix
	cfg.InternalNetV4 = section.InternalNetV4
	cfg.HealthStateFile = section.HealthStateFile
	cfg.ShadowMode = section.ShadowMode
	cfg.WANs = make([]wanroutes.WAN, 0, len(section.WAN))
	for i, wan := range section.WAN {
		if wan.FwMark < 0 {
			return wanroutes.Config{}, fmt.Errorf(
				"wan_routes.wan[%d].fw_mark must be >= 0",
				i,
			)
		}
		cfg.WANs = append(cfg.WANs, wanroutes.WAN{
			Name:       wan.Name,
			Iface:      wan.Iface,
			TableID:    wan.TableID,
			FwMark:     uint32(wan.FwMark),
			FwMarkPrio: wan.FwMarkPrio,
			FromPrio:   wan.FromPrio,
			NptPrefix:  wan.NptPrefix,
		})
	}
	return cfg, nil
}

func parseDurationSetting(
	raw string,
	defaultValue time.Duration,
	fieldName string,
) (time.Duration, error) {
	if raw == "" {
		return defaultValue, nil
	}
	durationValue, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", fieldName, raw, err)
	}
	return durationValue, nil
}

func parseAddrList(values []string, fieldName string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, len(values))
	for i, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] %q: %w", fieldName, i, value, err)
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}
