package awsmvm

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// LoadConfig resolves an AWS config through the standard SDK credential chain
// (environment, shared config, instance/IRSA role, and — inside a MicroVM — the
// execution-role container credentials endpoint). If region is non-empty it
// overrides the resolved region.
func LoadConfig(ctx context.Context, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}
	if cfg.Region == "" {
		return aws.Config{}, fmt.Errorf("AWS region not set (use --lambda-microvm-region or AWS_REGION)")
	}
	return cfg, nil
}
