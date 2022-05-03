# Lock Exec

`lock-exec` is a CLI that makes it easy to run at-most-once commands in a distributed environment. At Loom we run multiple identical Kubernetes clusters and we use `lock-exec` to ensure that Kubernetes cron jobs only run in a single cluster. `lock-exec` uses dynamodb to lock on a user specified key, run the input command, and then unlock the key.

## Requirements

You must already be authenticated to AWS with a default region. `lock-exec` uses [`config.LoadDefaultConfig`](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/config#LoadDefaultConfig) to load AWS credentials using the standard credential chain and does not currently support any direct method of authentication.

Additionally, `lock-exec` requires a dynamodb table to use for locking. This table must have a partition key named `id` that stores the lock keys. The default table name is `lock-exec`. If you use a different table name you must specify it explictly using the `--table` flag.

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
  --key-schema AttributeName=id,KeyType=HASH
  --attribute-definitions AttributeName=id,AttributeType=S
```

Terraform

```hcl
resource "aws_dynamodb_table" "lock_exec" {
  name = "lock-exec"

  table_class  = "STANDARD"
  billing_mode = "PAY_PER_REQUEST"

  hash_key = "id"

  attribute {
    name = "id"
    type = "S"
  }
}
```

## Installation

Download the `lock-exec` binary from the [releases page](https://github.com/loomhq/lock-exec/releases). You can also install using Go or Docker.

```shell
# Using Go
go install github.com/loomhq/lock-exec@latest

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

Once the command finishes running `lock-exec` will unlock the key. It also listens for os interrupts and unlocks the key before exiting. In the rare case where `lock-exec` exits and fails to unlock the key will remain locked for the next 24 hours. The key can manually be unlocked earlier using `lock-exec unlock <key>`.
