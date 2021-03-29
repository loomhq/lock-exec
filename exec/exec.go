package exec

import (
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"cirello.io/dynamolock"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/loomhq/lock-exec/utils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	leaseDuration = 5 // seconds

	heartBeatDuration = 1 // seconds

	minRandomSleep = 1000 // milliseconds - 1second
)

var execCommand = exec.Command

// Exec is a struct used to work with dynamo client interface
type Exec struct {
	dynamodbiface.DynamoDBAPI
}

// NewDynamoClient returns a Dynamo struct with client
// session
func NewDynamoClient(awsRegionName string) (*Exec, error) {
	sess := utils.AWSSession(awsRegionName)
	dynamoClient := dynamodb.New(sess)

	return &Exec{
		dynamoClient,
	}, nil
}

// Run first acquires the lock, executes the command and releases the lock.
// It returns the output into STDOUT & STDERR
// If sleepStartRandom & holdLockBy have non-zero values, it accordingly
// introduces randomized sleep before start and holds the lock by that duration
// before stop.
func (d *Exec) Run(tableName string, keyName string, command string, sleepStartRandom int, holdLockBy int) error {
	if sleepStartRandom > 0 {
		randomizeSleep(sleepStartRandom)
	}

	dl, err := dynamolock.New(
		d,
		tableName,
		dynamolock.WithLeaseDuration(leaseDuration*time.Second),
		dynamolock.WithHeartbeatPeriod(heartBeatDuration*time.Second),
	)
	if err != nil {
		return err
	}
	defer dl.Close()

	// Exec lock
	logrus.Info("Acquiring lock....")
	lockedItem, err := dl.AcquireLock(
		keyName,
		dynamolock.FailIfLocked(),
	)
	if err != nil {
		if _, ok := err.(*dynamolock.LockNotGrantedError); !ok {
			return err
		}

		// We still want to exit early, just not as an error.
		logrus.Warning(err)
		return nil
	}
	logrus.Info("Lock acquired")

	// Run Command
	logrus.WithFields(logrus.Fields{
		"command": command,
	}).Info("Executing command")
	out, cmdErr := runExec(command)

	// Remove the trailing newline (and any other extraneous whitespace)
	// so they don't pollute the log output.
	trimedOutput := strings.TrimSpace(out)

	// Always print output, regardless of error
	lines := strings.Split(trimedOutput, "\n")
	for _, l := range lines {
		logrus.WithFields(logrus.Fields{
			"command": command,
			"line":    l,
		}).Info("Command output")
	}

	if holdLockBy > 0 {
		sleepBy(holdLockBy)
	}

	logrus.Info("Releasing lock....")
	success, err := dl.ReleaseLock(lockedItem)
	if !success {
		return errors.New("lost lock before release")
	}
	if err != nil {
		return errors.Wrap(err, "error releasing lock")
	}
	logrus.Info("Lock released")

	// If there were no locking errors,
	// fallback to returning the error from the command.
	return cmdErr
}

// RunCommand executes the command.
// It returns the output into STDOUT
func runExec(command string) (string, error) {
	args := strings.Fields(command)

	cmd := execCommand(args[0], args[1:]...)

	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		return string(stdoutStderr), err

	}
	return string(stdoutStderr), nil
}

// randomizeSleep takes a int input as an upper bound and adds a
// randomized effect by sleeping for the random interval.
func randomizeSleep(i int) {
	rand.Seed(time.Now().UnixNano())
	ms := i * 1000
	offsetMS := 100                                             // milliseconds
	r := minRandomSleep + rand.Intn(ms-minRandomSleep+offsetMS) // ensures that the random range is [1, i]. To avoid sleeping for 0 seconds (0 ms).

	logrus.Infof("Randomized execution. Sleeping for %d milliseconds.", r)

	time.Sleep(time.Duration(r) * time.Millisecond)
}

// sleepBy takes a int input, and sleeps for that duration
func sleepBy(i int) {
	logrus.Infof("Holding lock - Sleeping for %d seconds.", i)

	time.Sleep(time.Duration(i) * time.Second)
}
