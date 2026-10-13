# Lock Exec

`lock-exec` is a CLI that makes it easy to run at-most-once commands in a distributed environment. At Loom we run multiple identical Kubernetes clusters and we use `lock-exec` to ensure that Kubernetes cron jobs only run in a single cluster. `lock-exec` uses dynamodb to lock on a user specified key, run the input command, and then unlock the key.

## Requirements

You must already be authenticated to AWS with a default region. `lock-exec` uses [`config.LoadDefaultConfig`](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/config#LoadDefaultConfig) to load AWS credentials using the standard credential chain and does not currently support any direct method of authentication.

Additionally, `lock-exec` requires a dynamodb table to use for locking. This table must have a partition key named `key` that stores the lock keys. The default table name is `lock-exec`. If you use a different table name you must specify it explictly using the `--table` flag. It is also possible to override the default AWS region using the `--region` flag.

### Required IAM Permissions

The following IAM permissions are required on the DynamoDB table containing the locks:

- `dynamodb:GetItem`
- `dynamodb:PutItem`
- `dynamodb:UpdateItem`
- `dynamodb:DeleteItem`

### Creating The Table

AWS CLI

```shell
aws dynamodb create-table --table-name lock-exec \
  --table-class STANDARD \
  --billing-mode PAY_PER_REQUEST \
  --key-schema AttributeName=key,KeyType=HASH
  --attribute-definitions AttributeName=key,AttributeType=S
```

Terraform

```hcl
resource "aws_dynamodb_table" "lock_exec" {
  name = "lock-exec"

  table_class  = "STANDARD"
  billing_mode = "PAY_PER_REQUEST"

  hash_key = "key"

  attribute {
    name = "key"
    type = "S"
  }
}
```

## Installation

Download the `lock-exec` binary from the [releases page](https://github.com/loomhq/lock-exec/releases). You can also install using Go or Docker.

```shell
# Using Go
go install github.com/loomhq/lock-exec/v2@latest

# Using Docker
docker run ghcr.io/loomhq/lock-exec --help
```

## Usage

Basic usage is `lock-exec run <key> <command>`.

```shell
$ go run main.go run testkey 'echo "hello world"' -t loomctl-locks
{"level":"info","ts":1651558192.655782,"caller":"cmd/run.go:22","msg":"running command","key":"testkey","command":"echo \"hello world\""}
{"level":"info","ts":1651558192.944402,"caller":"cmd/run.go:33","msg":"command succeeded","key":"testkey","command":"echo \"hello world\"","output":"\"hello world\"\n"}
```

Once the command finishes running `lock-exec` will unlock the key. It also listens for os interrupts and unlocks the key before exiting. In the rare case where `lock-exec` exits and fails to unlock the key will remain locked for the next 24 hours (you can customize this with `--expire <duration>`). The key can manually be unlocked earlier using `lock-exec unlock <key>`.

## Alternative backend: TDP ERS (experimental)

`lock-exec` is being migrated off DynamoDB and onto Atlassian's [TDP Entity Relationship
Store](https://developer.atlassian.com/platform/entity-relationship-store/) (ERS), which Loom can
reach from both AWS and GCP (see `INF-1998`). This is in-progress work, not yet used in production
(backend default is `dynamodb`).

Pass `--backend=ers` to use it instead:

```shell
lock-exec run --backend=ers mykey 'echo "hello world"'
```

Flags:

- `--backend dynamodb|ers` (default `dynamodb`)
- `--ers-env local|staging|production` (default `local`) -- which ERS to use. For `staging` and
  `production` this sets the endpoint (the `istio-egress-internal` gateway) and the ERS
  service-gateway host (`ers-data.sgw.staging.atl-paas.net` or `ers-data.sgw.prod.atl-paas.net`,
  unless `--ers-shard` names a non-default shard)
- `--ers-partition-id` -- the partition to write locks into. Required for `staging`/`production`;
  defaults to `tdp-ers-local` (the local dev seed) for `local`
- `--ers-region` -- the **partition's** ERS region, e.g. `stg-west2` or `prod-west2`. Required for
  `staging`/`production`
- `--ers-shard` -- the **partition's** ERS shard, e.g. `ers-data-prod-west2-1`. Only needed when the
  partition isn't on its region's default shard (`ers-data-<region>`); see **Shards** below
- `--ers-endpoint` -- optional override for the ERS base URL (defaults to
  `http://localhost:9300` for `local`; see **Local development** below)

### Staging

**1. Look up the partition.** The partition (`lock-exec-stg-west2`) and schema
(`ati:loom:infra:lock-exec`) are defined in `loom-tdp-artifacts`:

```shell
$ atlas tdp get-partitions -n lock-exec-stg-west2 -e staging -s loom -d ers
┌─────────┬─────────────┬────────────────────────────────────────┬───────────────────────────────────────────┬──────────────────────┐
│ (index) │ environment │              partitionId               │                productHost                │   productShardName   │
├─────────┼─────────────┼────────────────────────────────────────┼───────────────────────────────────────────┼──────────────────────┤
│    0    │ 'stg-west2' │ 'deb04125-c1a2-371e-b263-6a76f0a90e94' │ 'ers-data.us-west-2.staging.atl-paas.net' │ 'ers-data-stg-west2' │
└─────────┴─────────────┴────────────────────────────────────────┴───────────────────────────────────────────┴──────────────────────┘
```

Take these values from that row:

| Column | Flag |
|---|---|
| `partitionId` | `--ers-partition-id` |
| `environment` | `--ers-region` |
| `productShardName` | `--ers-shard` (only if it isn't `ers-data-<environment>`) |

**2. Run `lock-exec`** from a pod on a staging cluster. Requests go through the
`istio-egress-internal` gateway, which authenticates them as `loom/loom`:

```shell
lock-exec run --backend=ers \
  --ers-env=staging \
  --ers-partition-id=deb04125-c1a2-371e-b263-6a76f0a90e94 \
  --ers-region=stg-west2 \
  mykey 'echo "hello world"'
```

In a cron job, `--ers-env` can come from `${ARGOCD_ENV_CLUSTER_ENV}`, which is `staging` or
`production`.

`--ers-region` is where the **partition** lives, not where the cluster is. Every cluster in the
environment passes the same value (for staging, `stg-west2`), whether it runs in `us-west-2`,
`eu-central-1` or GCP. Don't pass the cluster's own region: ERS won't find the partition there.

This requires `ers-data.sgw.staging.atl-paas.net` in the cluster's `istio-egress-internal`
allowlist (`infra-apps`).

### Shards

Each ERS region has a default shard, `ers-data-<region>` (e.g. `ers-data-stg-west2`), reached
through `ers-data.sgw.<staging|prod>.atl-paas.net`. Some regions have extra shards (e.g.
`ers-data-prod-west2-1`), each with its own host, `<shard>.sgw.<staging|prod>.atl-paas.net`. A
partition can only be used through its own shard, so for a partition on a non-default shard pass
`--ers-shard`:

```shell
lock-exec run --backend=ers \
  --ers-env=production \
  --ers-partition-id=5137b20e-8af9-399f-9500-24cc60c309ab \
  --ers-region=prod-west2 \
  --ers-shard=ers-data-prod-west2-1 \
  mykey 'echo "hello world"'
```

`--ers-region` stays the region (`prod-west2`), not the shard. Typical errors when these are
wrong:

- `422 INVALID_PARTITION_SHARD_FOR_OPERATION`: the request reached the wrong shard, usually because
  `--ers-shard` is missing.
- `503 no healthy upstream`: the service gateway has no deployment for that host and region, e.g.
  `--ers-region` was set to a shard name like `prod-west2-1`.

The shard's host must also be in the cluster's `istio-egress-internal` allowlist.

### Local development

Run a local ERS instance using the [loom repo's own seeded setup
script](https://github.com/loomhq/loom/blob/main/projects/repo-tools/devenv/tdp/ers/start-local-tdp-ers.sh):

```shell
cd path/to/loom/projects/repo-tools/devenv/tdp/ers
./start-local-tdp-ers.sh
```

**Important:** always use this script (or otherwise pass `-d <seedingpath>` to
`atlas tdp ers local up` yourself) -- a bare `atlas tdp ers local up` with no seeding path silently
skips creating the `tdp-ers-local` partition `lock-exec` expects, which looks identical to a real
ERS problem from the outside (requests fail with unhelpful errors) unless you specifically check
how the instance was started.

**Important:** `atlas tdp ers local up` starts two separate services -- an ERS *control* plane
(schema/partition management) on port `9301`, and an ERS *data* plane (the actual node
create/read/update/delete API `lock-exec` talks to) on port `9300`. `lock-exec` only ever talks to
the data plane (`9300`); the control plane is only used by the seeding script and manual
`curl`/schema-management commands.

Run the ERS-backed integration tests (which exercise a full `Lock`/`Locked`/`Unlock`/`Run` cycle
against this real local instance, not a mock) with:

```shell
go test -tags ers_integration ./ers/... -v
```

### Status / caveats

- All clusters that share a lock key must use the same `--backend`. Locks in DynamoDB and ERS
  are separate, so mixing backends for one key lets a job run more than once.
- Production's partition (`lock-exec-prod-west2`) is on shard `ers-data-prod-west2-1`, so production
  needs `--ers-shard=ers-data-prod-west2-1` and `ers-data-prod-west2-1.sgw.prod.atl-paas.net` in the
  production egress allowlist.
- See `load-exec-project-scoping.md` in `infra-terraform` for the full migration plan, rollout
  phases, and design rationale.

## Package

The `lock` package can be utilized independently of the CLI tool. The package can be imported into a Go project using go get.
```shell
go get github.com/loomhq/lock-exec/v2
```
