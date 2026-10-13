package cmd

import (
	"fmt"
	"regexp"

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

// ersDefaultService is the micros service behind each region's default ERS shard
// (ers-data-<region>, e.g. ers-data-stg-west2), and the service-gateway host prefix for it.
const ersDefaultService = "ers-data"

// ersShardPattern matches ERS shard names: "ers-data" optionally followed by hyphenated
// lowercase alphanumeric parts, e.g. ers-data-prod-west2 or ers-data-prod-west2-1. The shard
// becomes part of the service-gateway host, so nothing else is accepted.
var ersShardPattern = regexp.MustCompile(`^ers-data(-[a-z0-9]+)*$`)

// ersOptions holds the --ers-* flags.
type ersOptions struct {
	env         string
	endpoint    string
	partitionID string
	region      string
	shard       string
}

// ersHost returns the ERS service-gateway host for env and the partition's shard and region, and
// an error for an unknown environment or an invalid shard. Note "prod", not "production", in the
// production host.
//
// Each region has a default shard named ers-data-<region> (e.g. ers-data-stg-west2), served by
// the "ers-data" micros service and reached through ers-data.sgw.<env>.atl-paas.net. Some
// regions have extra shards (e.g. ers-data-prod-west2-1): each is its own micros service with its
// own host, <shard>.sgw.<env>.atl-paas.net. A partition can only be read or written through its
// own shard. Going through the default host returns 422 INVALID_PARTITION_SHARD_FOR_OPERATION.
func ersHost(env, shard, region string) (string, error) {
	var suffix string

	switch env {
	case "staging":
		suffix = ".sgw.staging.atl-paas.net"
	case "production":
		suffix = ".sgw.prod.atl-paas.net"
	default:
		return "", fmt.Errorf("unknown --ers-env %q, expected local, staging or production", env)
	}

	if shard == "" || shard == ersDefaultService+"-"+region {
		return ersDefaultService + suffix, nil
	}

	if !ersShardPattern.MatchString(shard) {
		return "", fmt.Errorf("invalid --ers-shard %q, expected an ERS shard name such as ers-data-prod-west2-1", shard)
	}

	return shard + suffix, nil
}

// newERSStorage builds the ERS-backed storage client.
func (c *cli) newERSStorage() *ers.Storage {
	endpoint, headers, err := ersConfig(ersOptions{
		env:         c.ersEnv,
		endpoint:    c.ersEndpoint,
		partitionID: c.ersPartitionID,
		region:      c.ersRegion,
		shard:       c.ersShard,
	})
	c.fatalErr(err, "invalid ERS flags")

	client := ers.NewClient(nil, endpoint, ers.SchemaType, ers.SchemaVersion, headers)
	return ers.NewStorage(client)
}

// ersConfig resolves the ERS endpoint and headers from the flags.
//
// env is "local" (ERS-in-a-box), "staging" or "production". The endpoint (unless overridden)
// follows from env. The partition ID, region and shard don't: they describe the partition, so
// the ID and region must be passed explicitly for staging and production (the shard only for a
// partition outside the region's default shard), and they're the same for every cluster, whatever
// region the cluster itself is in. region and shard are ignored for local.
func ersConfig(opts ersOptions) (string, map[string]string, error) {
	endpoint := opts.endpoint

	if opts.env == "" || opts.env == "local" {
		if endpoint == "" {
			endpoint = ersLocalEndpoint
		}

		partitionID := opts.partitionID
		if partitionID == "" {
			partitionID = ersLocalPartitionID
		}

		return endpoint, ers.NewLocalDevHeaders(partitionID), nil
	}

	host, err := ersHost(opts.env, opts.shard, opts.region)
	if err != nil {
		return "", nil, err
	}

	if opts.partitionID == "" || opts.region == "" {
		return "", nil, fmt.Errorf("--ers-partition-id and --ers-region are required for --ers-env=%s", opts.env)
	}

	if endpoint == "" {
		endpoint = ersGatewayEndpoint
	}

	return endpoint, ers.NewProductionHeaders(host, opts.partitionID, opts.region), nil
}

// fatalErr logs the message and error and then exits if the error is not nil.
func (c *cli) fatalErr(err error, msg string) {
	if err == nil {
		return
	}

	c.log.Fatalf("%s: %v", err, msg)
}
