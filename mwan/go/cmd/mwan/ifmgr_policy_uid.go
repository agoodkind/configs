package main

import (
	"fmt"
	"os/user"
	"strconv"

	"goodkind.io/mwan/internal/config"
)

type userIDLookup func(username string) (string, error)

func buildPolicyRuleUIDRange(
	rule config.IfMgrPolicyRuleSection,
	lookup userIDLookup,
) (string, error) {
	if rule.UIDRange != "" && rule.UIDUser != "" {
		return "", fmt.Errorf("uid_range and uid_user are mutually exclusive")
	}
	if rule.UIDUser == "" {
		return rule.UIDRange, nil
	}
	uid, err := lookup(rule.UIDUser)
	if err != nil {
		return "", fmt.Errorf("lookup uid_user %q: %w", rule.UIDUser, err)
	}
	if _, err := strconv.ParseUint(uid, 10, 32); err != nil {
		return "", fmt.Errorf("uid_user %q resolved to invalid uid %q: %w",
			rule.UIDUser, uid, err)
	}
	return uid + "-" + uid, nil
}

func lookupUserID(username string) (string, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return account.Uid, nil
}
