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

// selftestHashModePath is the one config leaf the selftest publishes: the
// steering group's hash mode, set to its default value so a repeated run
// changes nothing an operator would notice.
const selftestHashModePath = "/ietf-interfaces:interfaces/goodkind-mwan-steering:steering-group/goodkind-mwan-steering:hash-mode"

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

	publishItems := []yangpub.Item{
		{Path: selftestHashModePath, Value: "random"},
	}
	if err := pub.SetItems(ctx, yangpub.DatastoreRunning, publishItems); err != nil {
		fmt.Fprintf(os.Stderr, "mwan wanconfig-selftest: publish: %v\n", err)
		return 1
	}
	log.Info("published selftest leaf", "path", selftestHashModePath)

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
