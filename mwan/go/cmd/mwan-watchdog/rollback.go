package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	preDeploySnapRE = regexp.MustCompile(`pre-deploy-[^\s]+`)
	knownGoodSnapRE = regexp.MustCompile(`known-good-[^\s]+`)
)

func extractLatestSnapshot(qmOutput []byte) string {
	s := string(qmOutput)
	pre := preDeploySnapRE.FindAllString(s, -1)
	if len(pre) > 0 {
		return pre[len(pre)-1]
	}
	kg := knownGoodSnapRE.FindAllString(s, -1)
	if len(kg) > 0 {
		return kg[len(kg)-1]
	}
	return ""
}

func parseRollbackStateFile(
	path string,
) (deployTS string, rollbackDone bool, snapshot string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, "", err
	}
	kv := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		kv[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return kv["deploy_timestamp"],
		kv["rollback_done"] == "true",
		kv["snapshot"],
		nil
}

func rollbackAlreadyDone(statePath string, deployTS int64) (bool, error) {
	ds := strconv.FormatInt(deployTS, 10)
	deployInFile, done, _, err := parseRollbackStateFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if deployInFile != ds {
		return false, nil
	}
	return done, nil
}

func writeRollbackState(path string, deployTS int64, snapshot string) error {
	content := fmt.Sprintf(
		"deploy_timestamp=%d\nrollback_done=true\nrollback_timestamp=%d\nsnapshot=%s\n",
		deployTS,
		time.Now().Unix(),
		snapshot,
	)
	return os.WriteFile(path, []byte(content), 0o644)
}
