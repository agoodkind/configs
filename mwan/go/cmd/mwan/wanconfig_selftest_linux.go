//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"goodkind.io/mwan/internal/wanconfig"
	"goodkind.io/mwan/internal/wanstate"
	"goodkind.io/mwan/internal/yangpub"
)

// selftestTimeout bounds the whole publish-and-provide exercise.
const selftestTimeout = 30 * time.Second

// selftestHoldTime keeps the provider registration alive long enough for
// an external RESTCONF read to hit it during testbed validation.
const selftestHoldTime = 20 * time.Second

// selftestHashModePath is the one config leaf the gateway selftest
// publishes, the steering group's hash mode, and selftestHashModeValue is
// what it writes there for the duration of the run.
const (
	selftestHashModePath  = "/ietf-interfaces:interfaces/goodkind-mwan-steering:steering-group/goodkind-mwan-steering:hash-mode"
	selftestHashModeValue = "random"
)

// restoreSelftestTimeout bounds the restore, which runs after the main
// context has already expired.
const restoreSelftestTimeout = 10 * time.Second

// selftestProviderModule and selftestProviderPath place a throwaway
// operational provider on the interfaces list, so an external read
// proves values reach the daemon at request time.
const (
	selftestProviderModule = "ietf-interfaces"
	selftestProviderPath   = "/ietf-interfaces:interfaces"
)

// selftestFlags selects the exercise. Without a repository the selftest
// runs against the host's datastore the way the testbed validation does;
// with one it stands up a private repository from the model files in
// modelsDir and proves the whole serving contract against it, touching
// nothing the host serves.
type selftestFlags struct {
	repository string
	modelsDir  string
}

// failStep logs a failed selftest step and returns it wrapped under the
// step's name, so the operator reads the step both in the journal and in
// the exit message.
func failStep(log *slog.Logger, step string, err error) error {
	log.Error("wanconfig selftest step failed", "step", step, "err", err)
	return fmt.Errorf("%s: %w", step, err)
}

func parseSelftestFlags(log *slog.Logger, args []string) (selftestFlags, error) {
	flags := selftestFlags{repository: "", modelsDir: ""}
	set := flag.NewFlagSet("wanconfig-selftest", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	set.StringVar(&flags.repository, "repository", "",
		"private sysrepo repository directory to create and test against, instead of the host's")
	set.StringVar(&flags.modelsDir, "models-dir", "",
		"directory holding the gateway's model files, installed into the private repository")
	if err := set.Parse(args); err != nil {
		return flags, failStep(log, "parse flags", err)
	}
	if (flags.repository == "") != (flags.modelsDir == "") {
		return flags, errors.New("--repository and --models-dir go together")
	}
	return flags, nil
}

// runWanconfigSelftest exercises the publishing binding end to end. On
// builds without the binding it reports unavailability and exits nonzero
// without touching anything.
func runWanconfigSelftest(args []string) int {
	log := slog.Default()
	flags, err := parseSelftestFlags(log, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: %v\n", err)
		return 2
	}
	if flags.repository != "" {
		return runPrivateSelftest(log, flags)
	}
	return runGatewaySelftest(log)
}

// runGatewaySelftest publishes one marker leaf into the host's running
// datastore, registers an operational provider, and holds the
// registration open for an external RESTCONF read.
func runGatewaySelftest(log *slog.Logger) int {
	pub, err := yangpub.New(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: %v\n", err)
		return 1
	}
	defer func() {
		if cerr := pub.Close(); cerr != nil {
			log.Error("yangpub close failed", "err", cerr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), selftestTimeout)
	defer cancel()

	// The selftest writes a real config leaf, so it snapshots the current
	// value first and puts it back on the way out. Without that, a run
	// against a gateway whose operator chose a non-default hash mode would
	// silently change how traffic is spread.
	priorValue, priorFound, err := pub.GetItem(ctx, yangpub.DatastoreRunning, selftestHashModePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: read current value: %v\n", err)
		return 1
	}
	defer restoreSelftestLeaf(pub, log, priorValue, priorFound)

	publishItems := []yangpub.Item{
		{Path: selftestHashModePath, Value: selftestHashModeValue},
	}
	if err := pub.SetItems(ctx, yangpub.DatastoreRunning, publishItems); err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: publish: %v\n", err)
		return 1
	}
	log.Info("published selftest leaf",
		"path", selftestHashModePath, "prior_value_present", priorFound)

	providerFn := func(_ context.Context, xpath string) ([]yangpub.Item, error) {
		log.Info("provider read", "xpath", xpath)
		requestTime := time.Now().UTC().Format(time.RFC3339)
		operItems := []yangpub.Item{
			{
				Path:  "/ietf-interfaces:interfaces/interface[name='wanconfig-selftest0']/type",
				Value: "iana-if-type:other",
			},
			{
				Path:  "/ietf-interfaces:interfaces/interface[name='wanconfig-selftest0']/description",
				Value: "computed at " + requestTime,
			},
		}
		return operItems, nil
	}
	if err := pub.RegisterProvider(ctx, selftestProviderModule, selftestProviderPath, providerFn); err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: register provider: %v\n", err)
		return 1
	}
	log.Info("provider registered, holding for external reads",
		"module", selftestProviderModule,
		"path", selftestProviderPath,
		"hold", selftestHoldTime.String())
	time.Sleep(selftestHoldTime)
	log.Info("wanconfig selftest complete")
	return 0
}

// restoreSelftestLeaf puts the hash mode back the way the selftest found
// it: the prior value when one was set, and no value at all when the leaf
// was absent and the schema default applied. A restore failure is logged
// rather than returned, because it runs from a deferred call after the
// exit code is decided.
func restoreSelftestLeaf(pub yangpub.Publisher, log *slog.Logger, priorValue string, priorFound bool) {
	ctx, cancel := context.WithTimeout(context.Background(), restoreSelftestTimeout)
	defer cancel()

	if priorFound {
		items := []yangpub.Item{{Path: selftestHashModePath, Value: priorValue}}
		if err := pub.SetItems(ctx, yangpub.DatastoreRunning, items); err != nil {
			log.Error("selftest leaf restore failed",
				"path", selftestHashModePath, "value", priorValue, "err", err)
			return
		}
		log.Info("selftest leaf restored", "path", selftestHashModePath, "value", priorValue)
		return
	}
	if err := pub.DeleteItem(ctx, yangpub.DatastoreRunning, selftestHashModePath); err != nil {
		log.Error("selftest leaf removal failed", "path", selftestHashModePath, "err", err)
		return
	}
	log.Info("selftest leaf removed", "path", selftestHashModePath)
}

// selftestModels lists the gateway's model files in install order, the
// order the deploy installs them, each matched by pattern because the
// file name carries the revision and a models directory may carry a
// different revision of the interface-type registry than the deploy ships.
var selftestModels = []struct {
	pattern  string
	features []string
}{
	{pattern: "ietf-yang-types@*.yang", features: nil},
	{pattern: "ietf-inet-types@*.yang", features: nil},
	{pattern: "ietf-interfaces@*.yang", features: nil},
	{pattern: "iana-if-type@*.yang", features: nil},
	{pattern: "ietf-ip@*.yang", features: nil},
	{pattern: "ietf-nat@*.yang", features: []string{"basic-nat44", "napt44", "dst-nat", "nptv6"}},
	{pattern: "goodkind-mwan-steering@*.yang", features: nil},
}

// resolveSelftestModels finds exactly one file per model in dir.
func resolveSelftestModels(log *slog.Logger, dir string) ([]yangpub.Model, error) {
	models := make([]yangpub.Model, 0, len(selftestModels))
	for _, entry := range selftestModels {
		matches, err := filepath.Glob(filepath.Join(dir, entry.pattern))
		if err != nil {
			return nil, failStep(log, "match "+entry.pattern, err)
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("want exactly one file matching %s in %s, found %d",
				entry.pattern, dir, len(matches))
		}
		models = append(models, yangpub.Model{Path: matches[0], Features: entry.features})
	}
	return models, nil
}

// selftestGateway is the shape the private selftest publishes: one
// translating member on tier 0 and the internal link, the smallest
// configuration that exercises every owned subtree.
func selftestGateway() wanconfig.Gateway {
	return wanconfig.Gateway{
		InternalIface: "eninternal0",
		Members: []wanconfig.Member{{
			Name:        "att",
			Iface:       "enatt0",
			Tier:        0,
			ProbePolicy: "att",
			NPTInternal: netip.MustParsePrefix("3d06:bad:b01:210::/60"),
			NPTExternal: netip.MustParsePrefix("2001:db8:a::/60"),
		}},
	}
}

// selftestStore is the live state the private selftest serves for that
// gateway, written the way the modules write it.
func selftestStore() *wanstate.Store {
	store := wanstate.New()
	store.SetHealth(map[string]wanstate.MemberHealth{
		"att": {
			Verdict:             wanstate.HealthHealthy,
			ConsecutiveFailures: 0,
			LastTransition:      time.Time{},
			V4:                  wanstate.ProbePass,
			V6:                  wanstate.ProbePass,
		},
	})
	store.SetRouting(0, map[string]wanstate.MemberRouting{"att": {Carrying: true}})
	store.SetTranslation(map[string]wanstate.MemberTranslation{
		"att": {Delegated: netip.MustParsePrefix("2001:db8:a::/60"), KernelPresent: true},
	})
	return store
}

// runPrivateSelftest proves the serving contract against a private
// repository: install the models, publish the configuration, own the
// modules, register the providers, and read the operational datastore
// over a second connection the way the RESTCONF server does. The read
// must carry the configuration and the live state together, a later
// publish must succeed with the ownership alive, and Close must release
// everything. Each failure names the step.
func runPrivateSelftest(log *slog.Logger, flags selftestFlags) int {
	if err := runPrivateSelftestSteps(log, flags); err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: %v\n", err)
		return 1
	}
	log.Info("wanconfig private selftest complete", "repository", flags.repository)
	return 0
}

func runPrivateSelftestSteps(log *slog.Logger, flags selftestFlags) error {
	if err := os.MkdirAll(flags.repository, 0o750); err != nil {
		return failStep(log, "create repository", err)
	}
	// sysrepo reads its repository path and shared-memory prefix from the
	// environment at connect time; a distinct prefix keeps this run apart
	// from any datastore the host serves.
	os.Setenv("SYSREPO_REPOSITORY_PATH", flags.repository)
	os.Setenv("SYSREPO_SHM_PREFIX", fmt.Sprintf("mwanselftest%d", os.Getpid()))

	ctx, cancel := context.WithTimeout(context.Background(), selftestTimeout)
	defer cancel()

	models, err := resolveSelftestModels(log, flags.modelsDir)
	if err != nil {
		return err
	}
	reader, err := yangpub.New(log)
	if err != nil {
		return failStep(log, "reader connection", err)
	}
	defer func() { _ = reader.Close() }()
	if err := reader.InstallModules(ctx, models, flags.modelsDir); err != nil {
		return failStep(log, "install models", err)
	}

	daemon, err := yangpub.New(log)
	if err != nil {
		return failStep(log, "daemon connection", err)
	}
	defer func() { _ = daemon.Close() }()

	gateway := selftestGateway()
	if err := wanconfig.Publish(ctx, log, runningReplacer{pub: daemon}, gateway); err != nil {
		return failStep(log, "publish configuration", err)
	}
	for _, module := range publishedModules {
		if err := daemon.OwnModule(ctx, module); err != nil {
			return failStep(log, "own "+module, err)
		}
	}
	if err := registerLiveStateProviders(ctx, log, daemon, selftestStore(), gateway); err != nil {
		return failStep(log, "register providers", err)
	}

	if err := checkSelftestTree(ctx, log, reader); err != nil {
		return err
	}
	if err := wanconfig.Publish(ctx, log, runningReplacer{pub: daemon}, gateway); err != nil {
		return failStep(log, "publish configuration with ownership alive", err)
	}
	if err := daemon.Close(); err != nil {
		return failStep(log, "close daemon connection", err)
	}
	after, found, err := reader.ExportJSON(ctx, yangpub.DatastoreOperational, "/ietf-interfaces:*")
	if err != nil {
		return failStep(log, "operational read after close", err)
	}
	if !found {
		return nil
	}
	// With nothing owned and nothing provided the datastore may still
	// print the bare container; what must be gone is the member's
	// configuration and its state.
	if err := checkSelftestInterfacesBare(log, json.RawMessage(after)); err != nil {
		return failStep(log, "interfaces tree after close: "+after, err)
	}
	return nil
}

// unmarshalObject decodes one JSON object level, so a check reaches into
// the tree without committing to its full shape.
func unmarshalObject(log *slog.Logger, raw json.RawMessage, what string) (map[string]json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, failStep(log, "decode "+what, err)
	}
	return object, nil
}

func unmarshalArray(log *slog.Logger, raw json.RawMessage, what string) ([]json.RawMessage, error) {
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err != nil {
		return nil, failStep(log, "decode "+what, err)
	}
	return array, nil
}

// expectLeaf compares one leaf's JSON encoding against the expected one.
func expectLeaf(object map[string]json.RawMessage, name string, want string, where string) error {
	got, present := object[name]
	if !present {
		return fmt.Errorf("%s: %s is absent", where, name)
	}
	if string(got) != want {
		return fmt.Errorf("%s: %s = %s, want %s", where, name, got, want)
	}
	return nil
}

// checkSelftestTree reads both owned subtrees from the operational
// datastore and checks that the configuration and the live state arrive
// together.
func checkSelftestTree(ctx context.Context, log *slog.Logger, reader yangpub.Publisher) error {
	interfacesJSON, found, err := reader.ExportJSON(ctx, yangpub.DatastoreOperational, "/ietf-interfaces:*")
	if err != nil {
		return failStep(log, "operational interfaces read", err)
	}
	if !found {
		return errors.New("operational interfaces read: nothing served")
	}
	if err := checkSelftestInterfaces(log, json.RawMessage(interfacesJSON)); err != nil {
		return failStep(log, "interfaces tree: "+interfacesJSON, err)
	}
	natJSON, found, err := reader.ExportJSON(ctx, yangpub.DatastoreOperational, "/ietf-nat:*")
	if err != nil {
		return failStep(log, "operational nat read", err)
	}
	if !found {
		return errors.New("operational nat read: nothing served")
	}
	if err := checkSelftestNAT(log, json.RawMessage(natJSON)); err != nil {
		return failStep(log, "nat tree: "+natJSON, err)
	}
	return nil
}

func checkSelftestInterfaces(log *slog.Logger, tree json.RawMessage) error {
	root, err := unmarshalObject(log, tree, "interfaces tree")
	if err != nil {
		return err
	}
	interfaces, err := unmarshalObject(log, root["ietf-interfaces:interfaces"], "interfaces container")
	if err != nil {
		return err
	}
	entries, err := unmarshalArray(log, interfaces["interface"], "interface list")
	if err != nil {
		return err
	}
	var member map[string]json.RawMessage
	for _, entry := range entries {
		decoded, err := unmarshalObject(log, entry, "interface entry")
		if err != nil {
			return err
		}
		if string(decoded["name"]) == `"enatt0"` {
			member = decoded
		}
	}
	if member == nil {
		return errors.New("interface enatt0 is absent")
	}
	steering, err := unmarshalObject(log, member["goodkind-mwan-steering:steering"], "steering container")
	if err != nil {
		return err
	}
	if err := expectLeaf(steering, "tier", "0", "configuration"); err != nil {
		return err
	}
	if err := expectLeaf(steering, "probe-policy", `"att"`, "configuration"); err != nil {
		return err
	}
	state, err := unmarshalObject(log, steering["state"], "steering state")
	if err != nil {
		return err
	}
	if err := expectLeaf(state, "health", `"healthy"`, "live state"); err != nil {
		return err
	}
	if err := expectLeaf(state, "carrying", "true", "live state"); err != nil {
		return err
	}
	group, err := unmarshalObject(log, interfaces["goodkind-mwan-steering:steering-group"], "steering group")
	if err != nil {
		return err
	}
	groupState, err := unmarshalObject(log, group["state"], "steering group state")
	if err != nil {
		return err
	}
	return expectLeaf(groupState, "active-tier", "0", "live state")
}

func checkSelftestNAT(log *slog.Logger, tree json.RawMessage) error {
	root, err := unmarshalObject(log, tree, "nat tree")
	if err != nil {
		return err
	}
	nat, err := unmarshalObject(log, root["ietf-nat:nat"], "nat container")
	if err != nil {
		return err
	}
	instances, err := unmarshalObject(log, nat["instances"], "instances container")
	if err != nil {
		return err
	}
	entries, err := unmarshalArray(log, instances["instance"], "instance list")
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return fmt.Errorf("nat instances = %d, want 1", len(entries))
	}
	instance, err := unmarshalObject(log, entries[0], "nat instance")
	if err != nil {
		return err
	}
	if err := expectLeaf(instance, "name", `"att"`, "configuration"); err != nil {
		return err
	}
	// The type is an identity of the instance's own module, which the JSON
	// encoding prints without a module prefix.
	if err := expectLeaf(instance, "type", `"nptv6"`, "configuration"); err != nil {
		return err
	}
	return expectLeaf(instance, "goodkind-mwan-steering:kernel-present", "true", "live state")
}

// checkSelftestInterfacesBare fails when the interfaces tree still carries
// the member's steering configuration or state.
func checkSelftestInterfacesBare(log *slog.Logger, tree json.RawMessage) error {
	root, err := unmarshalObject(log, tree, "interfaces tree")
	if err != nil {
		return err
	}
	raw, present := root["ietf-interfaces:interfaces"]
	if !present {
		return nil
	}
	interfaces, err := unmarshalObject(log, raw, "interfaces container")
	if err != nil {
		return err
	}
	rawList, present := interfaces["interface"]
	if !present {
		return nil
	}
	entries, err := unmarshalArray(log, rawList, "interface list")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		decoded, err := unmarshalObject(log, entry, "interface entry")
		if err != nil {
			return err
		}
		rawSteering, present := decoded["goodkind-mwan-steering:steering"]
		if !present {
			continue
		}
		steering, err := unmarshalObject(log, rawSteering, "steering container")
		if err != nil {
			return err
		}
		if _, present := steering["tier"]; present {
			return errors.New("configuration still enabled after close")
		}
		if _, present := steering["state"]; present {
			return errors.New("live state still served after close")
		}
	}
	return nil
}
