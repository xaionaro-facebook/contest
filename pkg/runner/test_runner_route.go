// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package runner

import (
	"container/list"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/facebookincubator/contest/pkg/event/testevent"
	"github.com/facebookincubator/contest/pkg/target"
	"github.com/facebookincubator/contest/pkg/test"
	"github.com/facebookincubator/contest/pkg/xcontext"
)

// router implements the routing logic that injects targets into a test step and consumes
// targets in output from another test step
type stepRouter struct {
	routingChannels routingCh
	bundle          test.TestStepBundle
	ev              testevent.EmitterFetcher

	timeouts TestRunnerTimeouts
}

// routeIn is responsible for accepting a target from the previous routing block
// and injecting it into the associated test step. Returns the number of targets
// injected into the test step or an error upon failure
func (r *stepRouter) routeIn(ctx xcontext.Context) (int, error) {
	stepLabel := r.bundle.TestStepLabel
	ctx = ctx.WithTag("phase", "routeIn").WithField("step", stepLabel)

	var (
		err             error
		injectionWg     sync.WaitGroup
		routeInProgress bool
	)

	// terminateTargetWriter is a control channel used to signal termination to
	// the writer object which injects a target into the test step
	terminateTargetWriterCtx, terminateTargetWriter := xcontext.WithCancel(xcontext.ResetSignalers(ctx))
	defer terminateTargetWriter() // avoids possible goroutine deadlock in context.WithCancel implementation

	// `targets` is used to buffer targets coming from the previous routing blocks,
	// queueing them for injection into the TestStep. The list is accessed
	// synchronously by a single goroutine.
	targets := list.New()

	// `ingressTarget` is used to keep track of ingress times of a target into a test step
	ingressTarget := make(map[string]time.Time)

	// Channel that the injection goroutine uses to communicate back to `routeIn` the results
	// of asynchronous injection
	injectResultCh := make(chan injectionResult)

	// injectionChannels are used to inject targets into test step and return results to `routeIn`
	injectionChannels := injectionCh{stepIn: r.routingChannels.stepIn, resultCh: injectResultCh}

	ctx.Logger().Debugf("initializing routeIn for %s", stepLabel)
	targetWriter := newTargetWriter(r.timeouts)

	for {
		select {
		case <-ctx.WaitFor():
			err = fmt.Errorf("termination requested for routing into %s", stepLabel)
		case injectionResult := <-injectResultCh:
			ctx.Logger().Debugf("received injection result for %v", injectionResult.target)
			routeInProgress = false
			if injectionResult.err != nil {
				err = fmt.Errorf("routing failed while injecting target %+v into %s", injectionResult.target, stepLabel)
				targetInErrEv := testevent.Data{EventName: target.EventTargetInErr, Target: injectionResult.target}
				if err := r.ev.Emit(ctx, targetInErrEv); err != nil {
					ctx.Logger().Warnf("could not emit %v event for target %+v: %v", targetInErrEv, *injectionResult.target, err)
				}
			} else {
				targetInEv := testevent.Data{EventName: target.EventTargetIn, Target: injectionResult.target}
				if err := r.ev.Emit(ctx, targetInEv); err != nil {
					ctx.Logger().Warnf("could not emit %v event for Target: %+v", targetInEv, *injectionResult.target)
				}
			}
		case t, chanIsOpen := <-r.routingChannels.routeIn:
			if !chanIsOpen {
				ctx.Logger().Debugf("routing input channel closed")
				r.routingChannels.routeIn = nil
			} else {
				ctx.Logger().Debugf("received target %v in input", t)
				targets.PushFront(t)
			}
		}

		if err != nil {
			break
		}

		if routeInProgress {
			continue
		}

		// no targets currently being injected in the test step
		if targets.Len() == 0 {
			if r.routingChannels.routeIn == nil {
				ctx.Logger().Debugf("input channel is closed and no more targets are available, closing step input channel")
				close(r.routingChannels.stepIn)
				break
			}
			continue
		}

		t := targets.Back().Value.(*target.Target)
		ingressTarget[t.ID] = time.Now()
		targets.Remove(targets.Back())
		ctx.Logger().Debugf("writing target %v into test step", t)
		routeInProgress = true
		injectionWg.Add(1)
		go func() {
			defer injectionWg.Done()
			targetWriter.writeTargetWithResult(terminateTargetWriterCtx, t, injectionChannels)
		}()
	}
	// Signal termination to the injection routines regardless of the result of the
	// routing. If the routing completed successfully, this is a no-op. If there is an
	// injection goroutine running, wait for it to terminate, as we might have gotten
	// here after a cancellation signal.
	terminateTargetWriter()
	injectionWg.Wait()

	if err != nil {
		ctx.Logger().Debugf("routeIn failed: %v", err)
		return 0, err
	}
	return len(ingressTarget), nil
}

func (r *stepRouter) emitOutEvent(ctx xcontext.Context, t *target.Target, err error) error {
	ctx = ctx.WithTag("phase", "emitOutEvent").WithField("step", r.bundle.TestStepLabel)

	if err != nil {
		targetErrPayload := target.ErrPayload{Error: err.Error()}
		payloadEncoded, err := json.Marshal(targetErrPayload)
		if err != nil {
			ctx.Logger().Warnf("could not encode target error ('%s'): %v", targetErrPayload, err)
		}
		rawPayload := json.RawMessage(payloadEncoded)
		targetErrEv := testevent.Data{EventName: target.EventTargetErr, Target: t, Payload: &rawPayload}
		if err := r.ev.Emit(ctx, targetErrEv); err != nil {
			return err
		}
	} else {
		targetOutEv := testevent.Data{EventName: target.EventTargetOut, Target: t}
		if err := r.ev.Emit(ctx, targetOutEv); err != nil {
			ctx.Logger().Warnf("could not emit %v event for target: %v", targetOutEv, *t)
		}
	}
	return nil
}

// routeOut is responsible for accepting a target from the associated test step
// and forward it to the next routing block. Returns the number of targets
// received from the test step or an error upon failure
func (r *stepRouter) routeOut(ctx xcontext.Context) (int, error) {

	stepLabel := r.bundle.TestStepLabel
	ctx = ctx.WithTag("phase", "routeOut").WithField("step", stepLabel)

	targetWriter := newTargetWriter(r.timeouts)

	var err error

	ctx.Logger().Debugf("initializing routeOut for %s", stepLabel)
	// `egressTarget` is used to keep track of egress times of a target from a test step
	egressTarget := make(map[string]time.Time)

	for {
		select {
		case <-ctx.WaitFor():
			err = fmt.Errorf("termination requested for routing into %s", r.bundle.TestStepLabel)
		case t, chanIsOpen := <-r.routingChannels.stepOut:
			if !chanIsOpen {
				ctx.Logger().Debugf("step output closed")
				r.routingChannels.stepOut = nil
				break
			}

			if _, targetPresent := egressTarget[t.ID]; targetPresent {
				err = fmt.Errorf("step %s returned target %+v multiple times", r.bundle.TestStepLabel, t)
				break
			}
			// Emit an event signaling that the target has left the TestStep
			if err := r.emitOutEvent(ctx, t, nil); err != nil {
				ctx.Logger().Warnf("could not emit out event for target %v: %v", *t, err)
			}
			// Register egress time and forward target to the next routing block
			egressTarget[t.ID] = time.Now()
			if err := targetWriter.writeTimeout(ctx, r.routingChannels.routeOut, t, r.timeouts.MessageTimeout); err != nil {
				ctx.Logger().Panicf("could not forward target to the test runner: %+v", err)
			}
		case targetError, chanIsOpen := <-r.routingChannels.stepErr:
			if !chanIsOpen {
				ctx.Logger().Debugf("step error closed")
				r.routingChannels.stepErr = nil
				break
			}

			if _, targetPresent := egressTarget[targetError.Target.ID]; targetPresent {
				err = fmt.Errorf("step %s returned target %+v multiple times", r.bundle.TestStepLabel, targetError.Target)
			} else {
				if err := r.emitOutEvent(ctx, targetError.Target, targetError.Err); err != nil {
					ctx.Logger().Warnf("could not emit err event for target: %v", *targetError.Target)
				}
				egressTarget[targetError.Target.ID] = time.Now()
				if err := targetWriter.writeTargetError(ctx, r.routingChannels.targetErr, targetError, r.timeouts.MessageTimeout); err != nil {
					log.Panicf("could not forward target (%+v) to the test runner: %v", targetError.Target, err)
				}
			}
		}
		if err != nil {
			break
		}
		if r.routingChannels.stepErr == nil && r.routingChannels.stepOut == nil {
			ctx.Logger().Debugf("output and error channel from step are closed, routeOut should terminate")
			close(r.routingChannels.routeOut)
			break
		}
	}

	if err != nil {
		ctx.Logger().Debugf("routeOut failed: %v", err)
		return 0, err
	}
	return len(egressTarget), nil

}

// route implements the routing logic from the previous routing block to the test step
// and from the test step to the next routing block
func (r *stepRouter) route(ctx xcontext.Context, resultCh chan<- routeResult) {

	var (
		inTargets, outTargets   int
		errRouteIn, errRouteOut error
	)

	terminateInternalCtx, terminateInternal := xcontext.WithCancel(ctx)
	defer terminateInternal() // avoids possible goroutine deadlock in context.WithCancel implementation

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		inTargets, errRouteIn = r.routeIn(terminateInternalCtx)
		if errRouteIn != nil {
			terminateInternal()
		}
	}()

	go func() {
		defer wg.Done()
		outTargets, errRouteOut = r.routeOut(terminateInternalCtx)
		if errRouteOut != nil {
			terminateInternal()
		}
	}()
	wg.Wait()

	routingErr := errRouteIn
	if routingErr == nil {
		routingErr = errRouteOut
	}
	if routingErr == nil && inTargets != outTargets {
		routingErr = fmt.Errorf("step %s completed but did not return all injected Targets (%d!=%d)", r.bundle.TestStepLabel, inTargets, outTargets)
	}

	// Send the result to the test runner, which is expected to be listening
	// within `MessageTimeout`. If that's not the case, we hit an unrecovrable
	// condition.
	select {
	case resultCh <- routeResult{bundle: r.bundle, err: routingErr}:
	case <-time.After(r.timeouts.MessageTimeout):
		log.Panicf("could not send routing block result")
	}
}

func newStepRouter(bundle test.TestStepBundle, routingChannels routingCh, ev testevent.EmitterFetcher, timeouts TestRunnerTimeouts) *stepRouter {
	r := stepRouter{bundle: bundle, routingChannels: routingChannels, ev: ev, timeouts: timeouts}
	return &r
}
