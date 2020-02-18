// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// +build go1.13

package frameworkevent_test

import (
	"testing"
	"time"

	"github.com/facebookincubator/contest/pkg/event/internal/unittest"

	. "github.com/facebookincubator/contest/pkg/event/frameworkevent"
)

var ( // to enable visibility of these types for TestQueryField_TypeConflicts
	_ = QueryJobID(0)
	_ = QueryEventNames(nil)
	_ = QueryEventName("")
	_ = QueryEmittedStartTime(time.Unix(1, 1))
	_ = QueryEmittedEndTime(time.Unix(1, 1))
)

// TestQueryField_TypeConflicts checks if every field of Query associated with
// exactly one QueryField.
func TestQueryField_TypeConflicts(t *testing.T) {
	unittest.TestQueryFieldTypesAreVisible(t)
	unittest.TestQueryFieldTypeConflicts(t, &[]QueryField{nil}[0])
}
