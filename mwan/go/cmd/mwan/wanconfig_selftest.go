package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"goodkind.io/mwan/internal/yangpub"
)

// selftestTimeout bounds the whole publish-and-provide exercise.
const selftestTimeout = 30 * time.Second

// selftestHoldTime keeps the provider registration alive long enough for
// an external RESTCONF read to hit it during testbed validation.
const selftestHoldTime = 20 * time.Second

// selftestHashModePath is the one config leaf the selftest publishes,
// the steering group's hash mode, and selftestHashModeValue is what it
// writes there for the duration of the run.
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

// runWanconfigSelftest exercises the publishing binding end to end: it
// publishes one marker leaf into the running datastore, registers an
// operational provider, and holds the registration open for an external
// RESTCONF read. On builds without the binding it reports
// unavailability and exits nonzero without touching anything.
func runWanconfigSelftest(_ []string) int {
	log := slog.Default()
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
