package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SbxTheDead/armada/internal/opclient"
)

// stringSlice is a repeatable string flag, e.g. --tag a --tag b.
type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// registerGlobals adds the shared --server/--token/--tenant flags to a flag set
// and returns a resolver that builds the client config once flags are parsed.
// Flag values take precedence over environment variables.
func registerGlobals(fs *flag.FlagSet) func() (opclient.Config, error) {
	server := fs.String("server", os.Getenv("ARMADA_SERVER_URL"), "control-plane base URL")
	token := fs.String("token", os.Getenv("ARMADA_OPERATOR_TOKEN"), "operator bearer token")
	tenant := fs.String("tenant", os.Getenv("ARMADA_TENANT_ID"), "tenant id")

	return func() (opclient.Config, error) {
		cfg := opclient.Config{
			BaseURL:  *server,
			Token:    *token,
			TenantID: *tenant,
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:8080"
		}
		if cfg.TenantID == "" {
			return opclient.Config{}, fmt.Errorf("tenant is required (set ARMADA_TENANT_ID or --tenant)")
		}
		return cfg, nil
	}
}
