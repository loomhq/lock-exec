package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/stretchr/testify/assert"
)

var (
	mockCmdStdout = "Expected output value"
)

type mockDynamoDBClient struct {
	dynamodbiface.DynamoDBAPI

	putItemWithContextCallback func(ctx aws.Context, input *dynamodb.PutItemInput, opts ...request.Option) (*dynamodb.PutItemOutput, error)

	getItemWithContext func(ctx aws.Context, input *dynamodb.GetItemInput, opts ...request.Option) (*dynamodb.GetItemOutput, error)

	updateItemWithContext func(ctx context.Context, input *dynamodb.UpdateItemInput, opts ...request.Option) (*dynamodb.UpdateItemOutput, error)
}

// Using these mocks we can simulate pass/fail runs when interacting with DDB.
// We are not testing for the lock accuracy/consistency (thats done in dynamolock)
func (c *mockDynamoDBClient) PutItemWithContext(ctx aws.Context, input *dynamodb.PutItemInput, opts ...request.Option) (*dynamodb.PutItemOutput, error) {
	return c.putItemWithContextCallback(ctx, input, nil)
}

func (c *mockDynamoDBClient) GetItemWithContext(ctx aws.Context, input *dynamodb.GetItemInput, opts ...request.Option) (*dynamodb.GetItemOutput, error) {
	return c.getItemWithContext(ctx, input, nil)
}

func (c *mockDynamoDBClient) UpdateItemWithContext(ctx context.Context, input *dynamodb.UpdateItemInput, opts ...request.Option) (*dynamodb.UpdateItemOutput, error) {
	return c.updateItemWithContext(ctx, input, nil)
}

// https://golang.org/src/os/exec/exec_test.go
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// TestHelperProcess is not a real test. It is a helper process for faking exec
// command execution. Its just unit testing the function, the command
// output itself doesn't matter.
// https://golang.org/src/os/exec/exec_test.go
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, mockCmdStdout)
	defer os.Exit(0)
}

func TestRunExec(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	out, err := runExec("foobar")

	assert.NoError(t, err)
	assert.Equal(t, out, mockCmdStdout)
}

func TestExecRun(t *testing.T) {
	t.Run("exec run passes", func(t *testing.T) {
		client := &mockDynamoDBClient{}
		client.putItemWithContextCallback = func(ctx aws.Context, input *dynamodb.PutItemInput, opts ...request.Option) (*dynamodb.PutItemOutput, error) {
			assert.Equal(t, "table-foo-bar", *input.TableName)
			assert.Equal(t, "custom-key", *input.Item["key"].S)

			return &dynamodb.PutItemOutput{}, nil
		}
		client.getItemWithContext = func(ctx aws.Context, input *dynamodb.GetItemInput, opts ...request.Option) (*dynamodb.GetItemOutput, error) {
			assert.Equal(t, "table-foo-bar", *input.TableName)
			assert.Equal(t, "custom-key", *input.Key["key"].S)

			return &dynamodb.GetItemOutput{}, nil
		}
		client.updateItemWithContext = func(ctx aws.Context, input *dynamodb.UpdateItemInput, opts ...request.Option) (*dynamodb.UpdateItemOutput, error) {
			assert.Equal(t, "table-foo-bar", *input.TableName)
			assert.Equal(t, "custom-key", *input.Key["key"].S)

			return &dynamodb.UpdateItemOutput{}, nil
		}
		d := Exec{
			client,
		}

		err := d.Run("table-foo-bar", "custom-key", "ls", 0, 0)
		assert.NoError(t, err)
	})

	t.Run("exec run fails", func(t *testing.T) {
		client := &mockDynamoDBClient{}
		client.getItemWithContext = func(ctx aws.Context, input *dynamodb.GetItemInput, opts ...request.Option) (*dynamodb.GetItemOutput, error) {
			return nil, awserr.New(dynamodb.ErrCodeConditionalCheckFailedException, "The conditional request failed.", errors.New("fake error"))
		}

		d := Exec{
			client,
		}

		err := d.Run("table-foo-bar", "custom-key", "ls", 0, 0)
		assert.Error(t, err)
	})
}

func TestExecRandomize(t *testing.T) {
	const secSleep = 2

	start := time.Now()
	randomizeSleep(int(secSleep))
	sec := time.Since(start).Seconds()

	if sec > (secSleep + 1) {
		t.Error("Randomized sleep is higher or incorrect.")
	}
}

func TestExecSleepBy(t *testing.T) {
	const secSleep = 1

	start := time.Now()
	sleepBy(int(secSleep))
	sec := time.Since(start).Seconds()

	if sec > (secSleep + 1) {
		t.Error("Randomized sleep is higher or incorrect.")
	}
}
