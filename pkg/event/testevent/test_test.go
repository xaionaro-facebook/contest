// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// +build go1.13

package testevent_test

import (
	"errors"
	"testing"
	"time"

	"github.com/facebookincubator/contest/pkg/event"
	"github.com/facebookincubator/contest/pkg/event/internal/unittest"
	"github.com/stretchr/testify/assert"

	. "github.com/facebookincubator/contest/pkg/event/testevent"
)

var ( // to enable visibility of these types for TestQueryField_TypeConflicts
	_ = QueryTestName("")
	_ = QueryTestStepLabel("")
)

// TestQueryField_TypeConflicts checks if every field of Query associated with
// exactly one QueryField.
func TestQueryField_TypeConflicts(t *testing.T) {
	unittest.TestQueryFieldTypesAreVisible(t)
	unittest.TestQueryFieldTypeConflicts(t, &[]QueryField{nil}[0])
}

func TestBuildQuery_Positive(t *testing.T) {
	_, err := QueryFields{
		QueryJobID(1),
		QueryEmittedStartTime(time.Now()),
		QueryEmittedEndTime(time.Now()),
	}.BuildQuery()
	assert.NoError(t, err)
}

func TestBuildQuery_NoDups(t *testing.T) {
	_, err := QueryFields{
		QueryJobID(2),
		QueryEmittedStartTime(time.Now()),
		QueryEmittedStartTime(time.Now()),
	}.BuildQuery()
	assert.Error(t, err)
	assert.True(t, errors.As(err, &event.ErrQueryFieldPassedTwice{}))

	_, err = QueryFields{
		QueryEventName("3"),
		QueryEventNames([]event.Name{"3"}),
	}.BuildQuery()
	assert.Error(t, err)
	assert.True(t, errors.As(err, &event.ErrQueryFieldPassedTwice{}))
}

func TestBuildQuery_NoZeroValues(t *testing.T) {
	_, err := QueryFields{
		QueryJobID(0),
		QueryEmittedStartTime(time.Now()),
		QueryEmittedEndTime(time.Now()),
	}.BuildQuery()
	assert.Error(t, err)
	assert.True(t, errors.As(err, &event.ErrQueryFieldHasZeroValue{}))
}
