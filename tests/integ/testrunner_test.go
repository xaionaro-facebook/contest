// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// +build integration

package tests

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/facebookincubator/contest/pkg/cerrors"
	"github.com/facebookincubator/contest/pkg/event"
	"github.com/facebookincubator/contest/pkg/logging"
	"github.com/facebookincubator/contest/pkg/pluginregistry"
	"github.com/facebookincubator/contest/pkg/runner"
	"github.com/facebookincubator/contest/pkg/storage"
	"github.com/facebookincubator/contest/pkg/target"
	"github.com/facebookincubator/contest/pkg/test"
	"github.com/facebookincubator/contest/pkg/types"

	"github.com/facebookincubator/contest/plugins/storage/memory"
	"github.com/facebookincubator/contest/plugins/teststeps/cmd"
	"github.com/facebookincubator/contest/plugins/teststeps/echo"
	"github.com/facebookincubator/contest/plugins/teststeps/example"
	"github.com/facebookincubator/contest/tests/plugins/teststeps/channels"
	"github.com/facebookincubator/contest/tests/plugins/teststeps/hanging"
	"github.com/facebookincubator/contest/tests/plugins/teststeps/noreturn"
	"github.com/facebookincubator/contest/tests/plugins/teststeps/panicstep"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	pluginRegistry *pluginregistry.PluginRegistry
	targets        []*target.Target
	successTimeout = 5 * time.Second
)

var testSteps = map[string]test.TestStepFactory{
	echo.Name:      echo.New,
	example.Name:   example.New,
	panicstep.Name: panicstep.New,
	noreturn.Name:  noreturn.New,
	hanging.Name:   hanging.New,
	channels.Name:  channels.New,
	cmd.Name:       cmd.New,
}

var testStepsEvents = map[string][]event.Name{
	echo.Name:      echo.Events,
	example.Name:   example.Events,
	panicstep.Name: panicstep.Events,
	noreturn.Name:  noreturn.Events,
	hanging.Name:   hanging.Events,
	channels.Name:  channels.Events,
	cmd.Name:       cmd.Events,
}

func TestMain(m *testing.M) {
	log := logging.GetLogger("tests/integ")
	log.Logger.Out = ioutil.Discard

	pluginRegistry = pluginregistry.NewPluginRegistry()
	// Setup the PluginRegistry by registering TestSteps
	for name, tsfactory := range testSteps {
		if _, ok := testStepsEvents[name]; !ok {
			err := fmt.Errorf("TestStep %s does not define any associated event", name)
			panic(err)
		}
		if err := pluginRegistry.RegisterTestStep(name, tsfactory, testStepsEvents[name]); err != nil {
			panic(fmt.Sprintf("could not register TestStep: %+v", err))
		}
	}
	// Setup test Targets and empty parameters
	targets = []*target.Target{
		&target.Target{Name: "host001", ID: "001", FQDN: "host001.facebook.com"},
		&target.Target{Name: "host002", ID: "002", FQDN: "host002.facebook.com"},
		&target.Target{Name: "host003", ID: "003", FQDN: "host003.facebook.com"},
		&target.Target{Name: "host004", ID: "004", FQDN: "host004.facebook.com"},
		&target.Target{Name: "host005", ID: "005", FQDN: "host005.facebook.com"},
	}

	// Configure the storage layer to be in-memory for TestRunner integration tests.
	// This layer persists across all tests.
	storage.SetStorage(memory.New())

	os.Exit(m.Run())
}

func TestSuccessfulCompletion(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)
	ts2, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)
	ts3, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, TestStepLabel: "FirstStage", Parameters: params},
		test.TestStepBundle{TestStep: ts2, TestStepLabel: "SecondStage", Parameters: params},
		test.TestStepBundle{TestStep: ts3, TestStepLabel: "ThirdStage", Parameters: params},
	}

	errCh := make(chan error)
	cancel := make(chan struct{})
	pause := make(chan struct{})

	go func() {
		tr := runner.NewTestRunner()
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()
	select {
	case err = <-errCh:
		require.NoError(t, err)
	case <-time.After(successTimeout):
		t.Errorf("test should return within timeout (%s)", successTimeout.String())
	}
}

func TestPanicStep(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("Panic")
	require.NoError(t, err)
	ts2, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, TestStepLabel: "StageOne", Parameters: params},
		test.TestStepBundle{TestStep: ts2, TestStepLabel: "StageTwo", Parameters: params},
	}

	cancel := make(chan struct{})
	pause := make(chan struct{})

	errCh := make(chan error)
	go func() {
		tr := runner.NewTestRunner()
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()
	select {
	case err = <-errCh:
		require.Error(t, err)
	case <-time.After(successTimeout):
		t.Errorf("test should return within timeout: %+v", successTimeout)
	}
}

func TestNoReturnStepWithCorrectTargetForwarding(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("NoReturn")
	require.NoError(t, err)
	ts2, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, Parameters: params},
		test.TestStepBundle{TestStep: ts2, Parameters: params},
	}

	cancel := make(chan struct{})
	pause := make(chan struct{})

	errCh := make(chan error)
	timeouts := runner.TestRunnerTimeouts{
		StepInjectTimeout:   30 * time.Second,
		MessageTimeout:      5 * time.Second,
		ShutdownTimeout:     1 * time.Second,
		StepShutdownTimeout: 1 * time.Second,
	}
	go func() {
		tr := runner.NewTestRunnerWithTimeouts(timeouts)
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()
	select {
	case err = <-errCh:
		require.Error(t, err)
		if _, ok := err.(*cerrors.ErrTestStepsNeverReturned); !ok {
			errString := fmt.Sprintf("Error returned by TestRunner should be of type ErrTestStepsNeverReturned: %v", err)
			assert.FailNow(t, errString)
		}
		assert.NotNil(t, err.(*cerrors.ErrTestStepsNeverReturned))
	case <-time.After(successTimeout):
		t.Errorf("test should return within timeout: %+v", successTimeout)
	}
}

func TestNoReturnStepWithoutTargetForwarding(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("Hanging")
	require.NoError(t, err)
	ts2, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, TestStepLabel: "StageOne", Parameters: params},
		test.TestStepBundle{TestStep: ts2, TestStepLabel: "StageTwo", Parameters: params},
	}

	cancel := make(chan struct{})
	pause := make(chan struct{})

	errCh := make(chan error)

	var (
		StepInjectTimeout   = 30 * time.Second
		MessageTimeout      = 5 * time.Second
		ShutdownTimeout     = 1 * time.Second
		StepShutdownTimeout = 1 * time.Second
	)
	timeouts := runner.TestRunnerTimeouts{
		StepInjectTimeout:   StepInjectTimeout,
		MessageTimeout:      MessageTimeout,
		ShutdownTimeout:     ShutdownTimeout,
		StepShutdownTimeout: StepShutdownTimeout,
	}

	go func() {
		tr := runner.NewTestRunnerWithTimeouts(timeouts)
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()

	testTimeout := 2 * time.Second
	testShutdownTimeout := ShutdownTimeout + StepShutdownTimeout + 2
	select {
	case err = <-errCh:
		// The TestRunner should never return and should instead time out
		assert.FailNow(t, "TestRunner should not return, received an error instead: %v", err)
	case <-time.After(testTimeout):
		// Assert cancellation signal and expect the TestRunner to return within
		// testShutdownTimeout
		close(cancel)
		select {
		case err = <-errCh:
			// The test timed out, which is an error from the perspective of the JobManager
			require.Error(t, err)
			if _, ok := err.(*cerrors.ErrTestStepsNeverReturned); !ok {
				errString := fmt.Sprintf("Error returned by TestRunner should be of type ErrTestStepsNeverReturned: %v", err)
				assert.FailNow(t, errString)
			}
		case <-time.After(testShutdownTimeout):
			assert.FailNow(t, "TestRunner should return after cancellation before timeout")
		}
	}
}

func TestStepClosesChannels(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("Channels")
	require.NoError(t, err)
	ts2, err := pluginRegistry.NewTestStep("Example")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, TestStepLabel: "StageOne", Parameters: params},
		test.TestStepBundle{TestStep: ts2, TestStepLabel: "StageTwo", Parameters: params},
	}

	cancel := make(chan struct{})
	pause := make(chan struct{})

	errCh := make(chan error)
	go func() {
		tr := runner.NewTestRunner()
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()

	testTimeout := 2 * time.Second
	select {
	case err = <-errCh:
		if _, ok := err.(*cerrors.ErrTestStepClosedChannels); !ok {
			errString := fmt.Sprintf("Error returned by TestRunner should be of type ErrTestStepClosedChannels: %v", err)
			assert.FailNow(t, errString)
		}
	case <-time.After(testTimeout):
		assert.FailNow(t, "TestRunner should not time out")
	}
}

func TestCmdPlugin(t *testing.T) {

	jobID := types.JobID(1)

	ts1, err := pluginRegistry.NewTestStep("cmd")
	require.NoError(t, err)

	params := make(test.TestStepParameters)
	params["executable"] = []test.Param{
		*test.NewParam("sleep"),
	}
	params["args"] = []test.Param{
		*test.NewParam("5"),
	}

	testSteps := []test.TestStepBundle{
		test.TestStepBundle{TestStep: ts1, Parameters: params},
	}

	cancel := make(chan struct{})
	pause := make(chan struct{})

	errCh := make(chan error)
	go func() {
		tr := runner.NewTestRunner()
		_, err := tr.Run(cancel, pause, &test.Test{TestStepsBundles: testSteps}, targets, jobID)
		errCh <- err
	}()

	go func() {
		time.Sleep(time.Second)
		close(cancel)
	}()

	select {
	case <-errCh:
	case <-time.After(successTimeout):
		t.Errorf("test should return within timeout: %+v", successTimeout)
	}
}
