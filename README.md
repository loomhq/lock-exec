A CLI tool for running any shell based commands in a distributed environment with DynamoDB locking.

## Requirements

- Go 1.14+
- DynamoDB Table
  - Ensure partition key is name `key`. An upstream requirement from `dynamolock`/

## Getting Started

```
 go get -u loomhq/lock-exec
```

## Defaults

- DynamoDB table name: `lock-exec`
- AWS Region: `us-west-2`

### Examples
```
A CLI tool for running any shell based commands in a distributed environment with DynamoDB locking

Usage:
  lock-exec [command]

Available Commands:
  help        Help about any command
  run         Run a shell command with acquire lock
  unlock      Force release an already acquired lock using key name

Flags:
  -h, --help            help for lock-exec
  -k, --key string      Name of the key (required)
  -r, --region string   AWS Region Name (default: "us-west-2")
  -t, --table string    Table Name (default: "lock-exec")

Use "lock-exec [command] --help" for more information about a command.
```

**Key name and command**
```
lock-exec run -k job-runner-foo -c "/usr/local/bin/do-something"
```

**Key name, region, table command**
```
lock-exec run-k job-runner-foo -r "us-west-2" -t "lock-exec" -c "/usr/local/bin/do-something"
```

## Overhead
- CPU: <1%
- Mem/RSS: ~30MB

Tested on local MacOS machine.

## Required DynamoDB Actions
The following IAM permissions are required on the DynamoDB table containing the locks:

- `GetItem`
- `PutItem`
- `UpdateItem`
- `DeleteItem`

## Random sleep and jitter like behavior

For use cases where the locking client is not atomic/fast enough, you can include a randomized sleep (with upper bound) to have jitter like behavior and stagger other execution when the task begins. Similarly, you can hold the lock after task completion to avoid other executions from acquiring the lock again. Examples

```
Usage:
  lock-exec run [flags]

Flags:
  -c, --command string           Shell Command (required)
  -h, --help                     help for run
  -l, --hold-lock int            Adds a sleep after running the command and before releasing the lock.
  -s, --sleep-start-random int   Adds a randomized sleep before running the command to add jitter like effect. Value in seconds and is the upper bound for the randomized sleep duration.

Global Flags:
  -k, --key string      Name of the key (required)
  -r, --region string   AWS Region Name (default: "us-west-2") (default "us-west-2")
  -t, --table string    Table Name (default: "lock-exec") (default "lock-exec")

```
#### Randomized sleep

```
lock-exec run-k job-runner-foo -r "us-west-2" -t "lock-exec" -c "/usr/local/bin/do-something" -s 10 // seconds
```

#### Holding lock post completion

```
lock-exec run-k job-runner-foo -r "us-west-2" -t "lock-exec" -c "/usr/local/bin/do-something" -l 60 // seconds
```

## Development & Release

### Requirements

Install goreleaser
```
brew install goreleaser/tap/goreleaser
```

### Publishing new release
Publishing of package is managed by goreleaser.

```bash
# Tag
git tag -a v0.1.0 -m 'v0.1.0'
git push origin v0.1.0

# release - from the root of your repository
export GITHUB_TOKEN=<>
goreleaser release
```
