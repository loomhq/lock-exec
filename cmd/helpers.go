package cmd

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/loomhq/lock-exec/v2/ers"
	"github.com/loomhq/lock-exec/v2/lock"
)

// newLocker returns a new lock client or logs and exits on failure. The storage backend used
// is controlled by --backend (dynamodb by default; ers is opt-in and does not affect any
// consumer that doesn't pass the flag).
func (c *cli) newLocker() *lock.Client {
	switch c.backend {
	case "", "dynamodb":
		return lock.New(c.newDynamoDBStorage(), c.table)
	case "ers":
		return lock.New(c.newERSStorage(), c.table)
	default:
		c.fatalErr(fmt.Errorf("unknown backend %q", c.backend), "invalid --backend flag")
		return nil
	}
}

// newDynamoDBStorage builds the existing DynamoDB-backed storage client.
func (c *cli) newDynamoDBStorage() *dynamodb.Client {
	options := [](func(*config.LoadOptions) error)(nil)
	if r := c.region; r != "" {
		options = append(options, config.WithRegion(r))
	}

	cfg, err := config.LoadDefaultConfig(c.cmd.Context(), options...)
	c.fatalErr(err, "failed to load aws config")

	return dynamodb.NewFromConfig(cfg)
}

const (
	// ersLocalEndpoint is local ERS-in-a-box's *data* plane (node CRUD -- what lock-exec needs).
	// `atlas tdp ers local up` also starts the *control* plane (schema/partition management) on
	// 9301; don't point lock-exec at that.
	ersLocalEndpoint = "http://localhost:9300"
	// ersLocalPartitionID matches the local dev seed.
	ersLocalPartitionID = "tdp-ers-local"
	// ersGatewayEndpoint is the istio-egress-internal gateway, the same in every cluster.
	ersGatewayEndpoint = "http://istio-egress-internal.istio-egress-internal.svc.cluster.local"
)

// ersHost returns the ERS service-gateway host for --ers-env, and false for an unknown
// environment. Note "prod", not "production", in the production host.
func ersHost(env string) (string, bool) {
	switch env {
	case "staging":
		return "ers-data.sgw.staging.atl-paas.net", true
	case "production":
		return "ers-data.sgw.prod.atl-paas.net", true
	default:
		return "", false
	}
}

// newERSStorage builds the ERS-backed storage client.
func (c *cli) newERSStorage() *ers.Storage {
	endpoint, headers, err := ersConfig(c.ersEnv, c.ersEndpoint, c.ersPartitionID, c.ersRegion)
	c.fatalErr(err, "invalid ERS flags")

	client := ers.NewClient(nil, endpoint, ers.SchemaType, ers.SchemaVersion, headers)
	return ers.NewStorage(client)
}

// ersConfig resolves the ERS endpoint and headers from the flags.
//
// env is "local" (ERS-in-a-box), "staging" or "production". The endpoint (unless overridden)
// and the service-gateway host follow from env. The partition ID and region don't: both
// describe the partition, so they must be passed explicitly for staging and production, and
// they're the same for every cluster, whatever region the cluster itself is in.
func ersConfig(env, endpoint, partitionID, region string) (string, map[string]string, error) {
	if env == "" || env == "local" {
		if endpoint == "" {
			endpoint = ersLocalEndpoint
		}

		if partitionID == "" {
			partitionID = ersLocalPartitionID
		}

		return endpoint, ers.NewLocalDevHeaders(partitionID), nil
	}

	host, ok := ersHost(env)
	if !ok {
		return "", nil, fmt.Errorf("unknown --ers-env %q, expected local, staging or production", env)
	}

	if partitionID == "" || region == "" {
		return "", nil, fmt.Errorf("--ers-partition-id and --ers-region are required for --ers-env=%s", env)
	}

	if endpoint == "" {
		endpoint = ersGatewayEndpoint
	}

	return endpoint, ers.NewProductionHeaders(host, partitionID, region), nil
}

// fatalErr logs the message and error and then exits if the error is not nil.
func (c *cli) fatalErr(err error, msg string) {
	if err == nil {
		return
	}

	c.log.Fatalf("%s: %v", err, msg)
}
