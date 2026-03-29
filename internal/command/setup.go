package command

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/configfile"
	"github.com/icholy/xagent/internal/deviceauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func defaultKeyName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "cli"
	}
	return hostname
}

var SetupCommand = &cli.Command{
	Name:  "setup",
	Usage: "Authenticate with the xagent server and generate keys",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:  "key-name",
			Usage: "Name for the API key",
			Value: defaultKeyName(),
		},
		&cli.IntFlag{
			Name:  "org",
			Usage: "Organization ID to use (prompted if not specified and user has multiple orgs)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// Load existing config (or empty if first run)
		cfg, err := configfile.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Generate private key if there isn't one
		if cfg.PrivateKey == nil {
			cfg.PrivateKey, err = agentauth.CreatePrivateKey()
			if err != nil {
				return fmt.Errorf("generate private key: %w", err)
			}
		}

		// Authenticate via device flow
		serverAddr := cmd.String("server")
		accessToken, err := deviceauth.DeviceFlow(ctx, deviceauth.DeviceFlowOptions{
			DiscoveryURL: deviceauth.DiscoveryURL(serverAddr),
			Display: func(resp *oidc.DeviceAuthorizationResponse) error {
				fmt.Printf("\nTo authenticate, visit: %s\n\n", resp.VerificationURIComplete)
				fmt.Println("Waiting for authentication...")
				return nil
			},
		})
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Create a client with the short-lived OIDC token
		client := xagentclient.New(xagentclient.Options{
			BaseURL:  serverAddr,
			Token:    accessToken,
			AuthType: "bearer",
		})

		// Fetch user profile to get available orgs
		profile, err := client.GetProfile(ctx, &xagentv1.GetProfileRequest{})
		if err != nil {
			return fmt.Errorf("get profile: %w", err)
		}

		// Select org
		orgID, err := selectOrg(profile, int64(cmd.Int("org")))
		if err != nil {
			return err
		}

		// Exchange the OIDC bearer token for an org-scoped app JWT
		tokenResp, err := xagentclient.GetToken(serverAddr, accessToken, orgID)
		if err != nil {
			return fmt.Errorf("token exchange: %w", err)
		}

		// Create a new client using the org-scoped app JWT
		client = xagentclient.New(xagentclient.Options{
			BaseURL:  serverAddr,
			Token:    tokenResp.Token,
			AuthType: "app",
		})

		// Create an API key scoped to the selected org
		resp, err := client.CreateKey(ctx, &xagentv1.CreateKeyRequest{
			Name: cmp.Or(cmd.String("key-name"), defaultKeyName()),
		})
		if err != nil {
			return fmt.Errorf("create API key: %w", err)
		}
		cfg.Token = resp.RawToken

		// Save config file
		if err := configfile.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		configPath, _ := configfile.Path()
		fmt.Printf("Config written to %s\n", configPath)

		// Create default workspaces.yaml if it doesn't exist
		if path, created, err := workspace.CreateDefault(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create default workspaces.yaml: %v\n", err)
		} else if created {
			fmt.Printf("Workspaces written to %s\n", path)
		}

		return nil
	},
}

// selectOrg picks an org from the user's profile.
// If orgID is specified, it validates the user is a member.
// If the user has one org, it's used automatically.
// Otherwise, the user is prompted to choose.
func selectOrg(profile *xagentv1.GetProfileResponse, orgID int64) (int64, error) {
	orgs := profile.Orgs
	if len(orgs) == 0 {
		return 0, fmt.Errorf("no organizations found for user")
	}

	// If --org flag was provided, validate membership
	if orgID != 0 {
		for _, org := range orgs {
			if org.Id == orgID {
				fmt.Printf("Using org: %s (id: %d)\n", org.Name, org.Id)
				return org.Id, nil
			}
		}
		return 0, fmt.Errorf("org %d not found in your organizations", orgID)
	}

	// Single org: use it automatically
	if len(orgs) == 1 {
		fmt.Printf("Using org: %s (id: %d)\n", orgs[0].Name, orgs[0].Id)
		return orgs[0].Id, nil
	}

	// Multiple orgs: prompt user to select
	fmt.Println("\nAvailable organizations:")
	for i, org := range orgs {
		marker := ""
		if org.Id == profile.DefaultOrgId {
			marker = " (default)"
		}
		fmt.Printf("  %d. %s (id: %d)%s\n", i+1, org.Name, org.Id, marker)
	}
	fmt.Printf("\nSelect org [1-%d]: ", len(orgs))

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return 0, fmt.Errorf("no input")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || choice < 1 || choice > len(orgs) {
		return 0, fmt.Errorf("invalid selection")
	}
	selected := orgs[choice-1]
	fmt.Printf("Using org: %s (id: %d)\n", selected.Name, selected.Id)
	return selected.Id, nil
}
