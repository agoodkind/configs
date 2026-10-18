package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5"
)

type connectionFile struct {
	name     string
	password string
	user     string
}

func readLine(directory, name string) string {
	value, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read the protected file\n", name)
		os.Exit(1)
	}
	return strings.TrimSuffix(string(value), "\n")
}

func checkPostgres(directory string, file connectionFile, hosts []string) bool {
	config, err := pgx.ParseConfig(readLine(directory, file.name))
	if err != nil {
		// Driver errors can include the connection string.
		fmt.Fprintf(os.Stderr, "%s: PostgreSQL rejected the connection string\n", file.name)
		return false
	}
	actualHosts := []string{config.Host}
	valid := config.Password == readLine(directory, file.password) &&
		config.User == file.user && config.Database == "tack" &&
		config.Port == 5433 && config.ConnectTimeout == time.Second && config.TLSConfig == nil
	for _, fallback := range config.Fallbacks {
		actualHosts = append(actualHosts, fallback.Host)
		valid = valid && fallback.Port == 5433 && fallback.TLSConfig == nil
	}
	if !valid || !reflect.DeepEqual(actualHosts, hosts) {
		fmt.Fprintf(os.Stderr, "%s: PostgreSQL parsed different credentials or connection settings\n", file.name)
		return false
	}
	return true
}

func checkClickHouse(directory string) bool {
	const name = "AUDIT_CONSUMER_CLICKHOUSE_DSN"
	config, err := clickhouse.ParseDSN(readLine(directory, name))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ClickHouse rejected the connection string")
		return false
	}
	if config.Auth.Password != readLine(directory, "CLICKHOUSE_PASSWORD") ||
		config.Auth.Username != "tack_audit_consumer" || config.Auth.Database != "audit" ||
		!reflect.DeepEqual(config.Addr, []string{"clickhouse:9000"}) {
		fmt.Fprintln(os.Stderr, "ClickHouse parsed different credentials or connection settings")
		return false
	}
	return true
}

func main() {
	if len(os.Args) != 3 || (os.Args[2] != "repointed" && os.Args[2] != "legacy") {
		fmt.Fprintln(os.Stderr, "Usage: check_connections DIRECTORY repointed|legacy")
		os.Exit(1)
	}
	directory := os.Args[1]
	serviceHosts := []string{"yugabyte"}
	operatorHosts := []string{"2001:db8::20"}
	if os.Args[2] == "repointed" {
		serviceHosts = []string{"yb1", "yb2", "yb3"}
		operatorHosts = []string{"2001:db8::21", "2001:db8::22", "2001:db8::23"}
	}
	valid := checkClickHouse(directory)
	for _, file := range []connectionFile{
		{"DATABASE_URL", "TACK_APP_PASSWORD", "tack_app"},
		{"AUDIT_CONSUMER_YUGABYTE_DSN", "AUDIT_WRITER_PASSWORD", "tack_audit_writer"},
		{"AUDIT_WRITER_DSN", "AUDIT_WRITER_PASSWORD", "tack_audit_writer"},
		{"AUDIT_READER_DSN", "AUDIT_READER_PASSWORD", "tack_audit_reader"},
		{"AUDIT_REDACTOR_DSN", "AUDIT_REDACTOR_PASSWORD", "tack_audit_redactor"},
		{"AUDIT_OPERATOR_DSN", "AUDIT_OPERATOR_PASSWORD", "tack_audit_operator"},
	} {
		if !checkPostgres(directory, file, serviceHosts) {
			valid = false
		}
	}
	for _, file := range []connectionFile{
		{"TACK_OPS_AUDIT_OPERATOR_DSN", "AUDIT_OPERATOR_PASSWORD", "tack_audit_operator"},
		{"TACK_OPS_AUDIT_READER_DSN", "AUDIT_READER_PASSWORD", "tack_audit_reader"},
		{"TACK_OPS_AUDIT_REDACTOR_DSN", "AUDIT_REDACTOR_PASSWORD", "tack_audit_redactor"},
		{"TACK_LEDGER_SUPERUSER_DATABASE_URL", "YUGABYTE_PASSWORD", "yugabyte"},
		{"TACK_OPS_DATABASE_URL", "TACK_MIGRATOR_PASSWORD", "tack_migrator"},
	} {
		if !checkPostgres(directory, file, operatorHosts) {
			valid = false
		}
	}
	if !valid {
		os.Exit(1)
	}
}
